/**
* 声明式 SOP -> 可执行计划
* 声明式的SOP唯一来源为 SKILL.md,设计与执行相分离
*
* 本文件是业务声明 -> 执行器 之间的唯一翻译器
*
* 目的:
*   - 把hCommon.SkillSOPStep 展开成Plan:拆解任务、算深度与波次、估预算、渲染、校验模版引用
*	  0 LLM < 1 ms
**/

package plan_mode

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"my.favorite/cclawservice/biz/npa"
	hCommon "my.favorite/cclawservice/harness/common"
	pCommon "my.favorite/platformutility/common"
)

const (
	defaultMaxTasks        = 32
	defaultChapterTokens   = 2000     // 章节预算(约 6K中文字符)
	defaultToolReplyTokens = 8 * 1024 // 工具回执上限(与工具层一致)

	defaultRoundP95   = 8 * time.Second // 无观测数据时的单轮耗时假设
	mergeTaskIDSuffix = "_merge"
)

// 模版引用:${spec.archive_no} / ${parsed_context.path}
var templateRefPattern = regexp.MustCompile(`\$\{([a-zA-Z0-9_]+)\.([a-zA-Z0-9_]+)}`)

// DefaultPlanArguments 默认的计划展开参数
func DefaultPlanArguments() PlanArguments {
	return PlanArguments{
		MaxTasks:           defaultMaxTasks,
		ChapterTokens:      defaultChapterTokens,
		ToolReplyTokens:    defaultToolReplyTokens,
		RoundP95:           defaultRoundP95,
		SequentialSections: true,
	}
}

func (args PlanArguments) normalize() PlanArguments {
	if args.MaxTasks <= 0 {
		args.MaxTasks = defaultMaxTasks
	}

	if args.ChapterTokens <= 0 {
		args.ChapterTokens = defaultChapterTokens
	}

	if args.ToolReplyTokens <= 0 {
		args.ToolReplyTokens = defaultToolReplyTokens
	}
	if args.RoundP95 <= 0 {
		args.RoundP95 = defaultRoundP95
	}
	return args
}

// BuildPlanFromSkillSOPOutline 把SKILL.md的SOP outline展开成可执行的计划
func BuildPlanFromSkillSOPOutline(skillId string, sopSteps []hCommon.SkillSOPStep, spec npa.ReportSpec, args PlanArguments) (Plan, error) {
	args = args.normalize()

	// 0.工具集是L3的唯一依据;没有传的话就返回错误
	if len(args.KnownTools) == 0 {
		for _, step := range sopSteps {
			if strings.EqualFold(strings.TrimSpace(step.Kind), hCommon.SOPStepKindTool) {
				return Plan{}, fmt.Errorf("plan invalid:KnownTools no registry,can not verify")
			}
		}
	}

	// 1. 结构校验
	if issues := ValidateNPAOutline(sopSteps, args.KnownTools); len(issues) > 0 {
		parts := make([]string, 0, len(issues))
		for _, issue := range issues {
			parts = append(parts, issue.String())
		}

		return Plan{}, fmt.Errorf("plan invalid:%s", strings.Join(parts, ","))
	}

	// 2.逐步转换
	tasks := make([]PlanTask, 0, len(sopSteps))
	conditional := 0

	for _, step := range sopSteps {
		kind := getPlanTaskByStepKind(step.Kind)
		when := strings.TrimSpace(step.When)

		planTask := PlanTask{
			ID:          strings.TrimSpace(step.ID),
			Kind:        kind,
			Action:      getActionFromSkillSOP(kind, step),
			Needs:       append([]string(nil), step.Needs...),
			Produces:    strings.TrimSpace(step.Produces),
			Args:        buildArgs(step.Args, spec),
			Checks:      append([]string(nil), step.Checks...),
			Gate:        strings.TrimSpace(step.Gate),
			Policy:      strings.TrimSpace(step.Policy),
			Contract:    getContractFromSkillSOP(step),
			When:        when,
			Conditional: when != pCommon.BlankString,
			MaxTries:    getMaxTries(kind, step.MaxTries),
		}

		if planTask.Conditional {
			conditional++
		}

		// kind = llm
		if kind == PlanTaskLLM && len(step.Sections) > 0 {
			tasks = append(tasks, expandSections(planTask, step.Sections, args)...)
			if merged, ok := buildMergeTasks(step); ok {
				tasks = append(tasks, merged)
			}
			continue
		}

		tasks = append(tasks, planTask)
	}

	if len(tasks) > args.MaxTasks {
		return Plan{}, fmt.Errorf("plan invalid:task number %d overflow the limit %d", len(tasks), args.MaxTasks)
	}

	// 4. 深度与波次
	if err := assignDepth(tasks); err != nil {
		return Plan{}, err
	}

	plan := Plan{SkillID: skillId, Tasks: tasks, Conditional: conditional}
	plan.Waves = getWaves(tasks)

	// 5. 模版引用校验(拼错产物名 -> 构建期就报错)
	if err := validateTemplate(tasks); err != nil {
		return Plan{}, err
	}

	// 6. 预算估算
	plan.PlanPayload = estimatePlanPayload(tasks, plan.Waves, args)

	return plan, nil
}

// ======================== private functions & methods ========================

func getActionFromSkillSOP(kind PlanTaskKind, step hCommon.SkillSOPStep) string {
	switch kind {
	case PlanTaskTool:
		return strings.TrimSpace(step.Tool)
	case PlanTaskLLM:
		if role := strings.TrimSpace(step.Role); role != pCommon.BlankString {
			return role
		}
		return "analyst"
	case PlanTaskCheck:
		return "check"
	default:
		return pCommon.BlankString
	}
}

func getPlanTaskByStepKind(kind string) PlanTaskKind {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case hCommon.SOPStepKindTool:
		return PlanTaskTool
	case hCommon.SOPStepKindLLM:
		return PlanTaskLLM
	case hCommon.SOPStepKindCheck:
		return PlanTaskCheck
	default:
		return 0
	}
}

func getContractFromSkillSOP(sopStep hCommon.SkillSOPStep) hCommon.SOPStepContract {
	if sopStep.Contract == nil {
		return hCommon.SOPStepContract{}
	}

	return *sopStep.Contract
}

// 工具步允许重试(SOP 契约):最多 2 次;LLM /check 一次为佳
func getMaxTries(kind PlanTaskKind, maxTries int) int {
	if kind == PlanTaskTool {
		if maxTries > 0 {
			return maxTries
		}

		return 2
	}

	return 1
}

func getWaves(tasks []PlanTask) int {
	waves := 0
	for _, task := range tasks {
		if task.Conditional {
			continue
		}

		if task.Depth+1 > waves {
			waves = task.Depth + 1
		}
	}
	return waves
}

// 构建已知的 ${spec.*};${artifact.field}留给运行期
func buildArgs(args hCommon.SOPStepArgs, spec npa.ReportSpec) map[string]string {
	if len(args) == 0 {
		return nil
	}

	fields := spec.Fields()
	out := make(map[string]string, len(args))
	for key, arg := range args {
		out[key] = buildSpecTemplate(arg, fields)
	}

	return out
}

func buildSpecTemplate(template string, fields map[string]string) string {
	return templateRefPattern.ReplaceAllStringFunc(template, func(match string) string {
		parts := templateRefPattern.FindStringSubmatch(match)
		if len(parts) != 3 || parts[1] != "spec" {
			return match // 非spec引用:原样保留,留给运行期
		}

		if value, ok := fields[parts[2]]; ok {
			return value
		}

		return match
	})
}

/** 为了"声明了 sections的产物"生成确定性的拼接任务
*
* merge:此章节必须按照声明顺序拼接,这是SOP确定性折叠 -- 若交给LLM会引入不确定性,
* 会让"最后一节顺带产出计划不可读、不可断点"
**/
func buildMergeTasks(step hCommon.SkillSOPStep) (PlanTask, bool) {
	parentID := strings.TrimSpace(step.ID)
	produces := strings.TrimSpace(step.Produces)
	if produces == pCommon.BlankString {
		return PlanTask{}, false
	}

	needs := make([]string, 0, len(step.Sections))
	for _, section := range step.Sections {
		name := strings.TrimSpace(section)
		if name != pCommon.BlankString {
			needs = append(needs, produces+"#"+name)
		}
	}

	merged := PlanTask{
		ID:       parentID + mergeTaskIDSuffix,
		ParentID: parentID,
		Kind:     PlanTaskMerge,
		Action:   produces,
		Needs:    needs,
		Produces: produces,
		MaxTries: 1,
	}

	// 继承原来步骤的门禁策略
	merged.Gate = strings.TrimSpace(step.Gate)
	merged.Policy = strings.TrimSpace(step.Policy)

	return merged, true
}

/** 把kind==llm的步骤按章节展开
*
* 每个章节任务:
*	- ID=<step>#<section>,便于修复回路
*	- Produces=<step.produces>#<section>:章节产物独立存放
*	- 串行模式下额外依赖上一章节的产物,保证"结论能引用前文"
**/
func expandSections(planTask PlanTask, sections []string, planArgs PlanArguments) []PlanTask {
	expanded := make([]PlanTask, 0, len(sections))
	previous := pCommon.BlankString

	for _, section := range sections {
		name := strings.TrimSpace(section)
		if name == pCommon.BlankString {
			continue
		}

		child := planTask
		child.ID = planTask.ID + "#" + name
		child.ParentID = planTask.ID
		child.Section = name
		child.Produces = planTask.Produces + "#" + name
		child.Needs = append([]string(nil), planTask.Needs...)

		if planArgs.SequentialSections && previous != pCommon.BlankString {
			child.Needs = append(child.Needs, planTask.Produces+"#"+previous)
		}

		expanded = append(expanded, child)

		previous = name
	}

	return expanded
}

/**
* 计算每个任务的深度:Depth=1+max(依赖产物的可调度者深度)
* 用拓扑DFS而非"顺序扫描 + lastProducer":后者要求生产者一定要声明在消费者之前
**/
func assignDepth(tasks []PlanTask) error {
	producers := make(map[string]int, len(tasks))
	for idx := range tasks {
		if tasks[idx].Conditional {
			continue
		}

		if name := strings.TrimSpace(tasks[idx].Produces); name != pCommon.BlankString {
			producers[name] = idx // 同名产物后来者覆盖
		}
	}

	depths := make([]int, len(tasks))
	states := make([]uint8, len(tasks)) // 0 未访问 1 访问中 2 完成

	var visit func(idx int) (int, error)
	visit = func(idx int) (depth int, err error) {
		switch states[idx] {
		case 2:
			return depths[idx], nil
		case 1:
			return 0, fmt.Errorf("plan invalid:cycle detected at task %s", tasks[idx].ID)
		}

		states[idx] = 1
		depths[idx] = 0

		for _, need := range tasks[idx].Needs {
			producerIdx, ok := producers[strings.TrimSpace(need)]
			if !ok || producerIdx == idx {
				continue // 未被任何可调度任务产出或自依赖:跳过
			}

			producerDepth, err := visit(producerIdx)
			if err != nil {
				return 0, err
			}

			// 注意比较 producerDepth+1:若只写 producerDepth > depths[idx],
			// 当生产者深度为 0 时 0 > 0 不成立 -> 整条链的深度全塌成 0(波次/预算跟着错)。
			if producerDepth+1 > depths[idx] {
				depths[idx] = producerDepth + 1
			}
		}

		states[idx] = 2 // 完成标记
		return depths[idx], nil
	}

	for idx := range tasks {
		if tasks[idx].Conditional {
			continue
		}

		if _, err := visit(idx); err != nil {
			return err
		}

		tasks[idx].Depth = depths[idx]
	}

	return nil
}

// 校验剩余的${artifact.field}引用都能被本计划产出
// 拼接产物名时最常见的故障,必须在构建期拦截
func validateTemplate(planTasks []PlanTask) error {
	produced := make(map[string]struct{}, len(planTasks))
	for _, planTask := range planTasks {
		if name := planTask.Produces; name != pCommon.BlankString {
			produced[name] = struct{}{}
		}
	}

	missing := make([]string, 0, 4)
	for _, planTask := range planTasks {
		for key, value := range planTask.Args {
			for _, parts := range templateRefPattern.FindAllStringSubmatch(value, -1) {
				name := parts[1]
				if name == "spec" {
					continue
				}

				if _, ok := produced[name]; !ok {
					missing = append(missing, fmt.Sprintf("%s.args.%s -> ${%s.%s}", planTask.ID, key, name, parts[2]))
				}
			}
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("plan invalid:referred the outcomes not produced:%s", strings.Join(missing, ","))
	}

	return nil
}

func estimatePlanPayload(planTasks []PlanTask, waves int, planArgs PlanArguments) PlanPayload {
	workPayload := PlanPayload{TaskCount: len(planTasks), WaveCount: waves}
	tokens := 0

	for _, planTask := range planTasks {
		switch planTask.Kind {
		case PlanTaskLLM:
			tokens += planArgs.ChapterTokens
		case PlanTaskTool:
			tokens += planArgs.ToolReplyTokens
		case PlanTaskMerge, PlanTaskCheck:
			//
		}
	}

	workPayload.EstTokens = int64(tokens)
	workPayload.EstLatency = time.Duration(waves) * planArgs.RoundP95

	return workPayload
}
