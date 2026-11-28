/**
* plan mode场景下定义的工具函数
**/

package plan_mode

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	hCommon "my.favorite/cclawservice/harness/common"
	pCommon "my.favorite/platformutility/common"
)

func GetSOPStepSummary(steps []hCommon.SkillSOPStep) []string {
	lines := make([]string, 0, len(steps))
	for _, step := range steps {
		action := step.Tool
		if step.Kind == hCommon.SOPStepKindLLM {
			action = step.Role
		}
		if action == pCommon.BlankString {
			action = step.Kind
		}

		line := fmt.Sprintf("[%s] %s -> %s", step.Kind, step.ID, action)
		if step.Produces != pCommon.BlankString {
			line += " (产出 " + step.Produces + ")"
		}
		if step.Gate != pCommon.BlankString {
			line += " (门禁 " + step.Gate + ")"
		}

		lines = append(lines, line)
	}

	return lines
}

func BuildIntentionSystemPrompt(req NegotiationRequest) string {
	var builder strings.Builder

	builder.WriteString("你是技能调度判定器。你只需要做两件事：\n")
	builder.WriteString("1）判断用户这句话是否需要执行本技能的SOP流程；")
	builder.WriteString("2）若需要执行，给出执行所需的参数。\n\n")

	builder.WriteString("【技能】" + req.SkillId + "\n")
	if req.SkillName != pCommon.BlankString {
		builder.WriteString("【名称】" + req.SkillName + "\n")
	}
	if req.SkillDesc != pCommon.BlankString {
		builder.WriteString("【简介】" + req.SkillDesc + "\n")
	}

	if len(req.SOPSteps) > 0 {
		builder.WriteString("\n【SOP 流程】\n")
		for i, step := range req.SOPSteps {
			builder.WriteString(fmt.Sprintf("%d. %s\n", i+1, step))
		}
	}

	// 让模型知道到哪里找东西
	if len(req.SystemToolExtraFacts) > 0 {
		keys := make([]string, 0, len(req.SystemToolExtraFacts))
		for key := range req.SystemToolExtraFacts {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		builder.WriteString("\n【已知事实】\n")
		for _, key := range keys {
			builder.WriteString(fmt.Sprintf("- %s = %s\n", key, req.SystemToolExtraFacts[key]))
		}

		if path := strings.TrimSpace(req.SystemToolExtraFacts["detected_path"]); path != pCommon.BlankString {
			builder.WriteString("- 说明：detected_path 是用户原话里已给出的绝对路径，**已经核实**；可直接作为project_dir，不需要额外任何探测。\n")
		}
	}

	if names := restoreSystemToolNames(req); len(names) > 0 {
		builder.WriteString("\n【可用工具】" + strings.Join(names, ",") + "：只读探测，用于确认材料是否存在、在哪里；不返回文件正文。\n")
	} else {
		builder.WriteString("\n【可用工具】本轮没有可用的只读探测工具,不要编造路径。\n")
	}

	// 只保留机制性规则:可被校验器/schema 强化;业务判据走 IntentGuard
	builder.WriteString("\n【机制规则】\n")
	builder.WriteString("1. 必须调用 " + IntentionToolStart + " 表态,不要用普通文本回答。\n")
	builder.WriteString("2. 用户已给出绝对路径时：直接用它作为 project_dir，**不需要任何探测**。\n")
	builder.WriteString("3. 只给债务人姓名时：用 glob 在【已知事实 materials_root】下按 pattern=\"*<姓名>*/**\" 找**一次**；\n")
	builder.WriteString("3.1 用户一次点名**多个目标**（如“罗勇兴、张三等多个债务人”）时：\n")
	builder.WriteString("   (1) 每个目标各探测**一次**，结果写进 targets（name / project_dir / found / reason）：name必须是**债务人姓名**（如罗勇兴），不是目录名（10罗勇兴）；\n")
	builder.WriteString("   (2) **只要至少一个目标有材料**，就 decision=proceed，spec.project_dir 填**第一个可用目标**的绝对路径；\n")
	builder.WriteString("   (3) 未找到的目标**不得阻断执行**，也不要 proceed 到不存在的目录：写进 targets(found=false)+reason；\n")
	builder.WriteString("   (4) 其余有材料但本轮不执行的目标也写进 targets(found=true)，reason 写“本轮不执行（可作为下一次请求）”；\n")
	builder.WriteString("   (5) 只有**全部**目标都没有材料时，才 decision=need_more_info，missing 里逐条列出候选绝对路径或追问项。\n")
	builder.WriteString("   (6) 能一次查完就一次查完：多目标可合并成一次 glob，例如 pattern=\"*{罗勇兴,张三,李四}*/**\"，避免逐个人探测。\n")
	builder.WriteString("4. 探测到目标目录里没有 schema 声明的材料扩展名(.pdf/.docx/.xlsx/.csv/.jpg/.png/.tif…)时，\n")
	builder.WriteString("   必须 decision=need_more_info，**不要 proceed**。\n")
	builder.WriteString("5. project_dir 必须是**绝对路径**，且与探测结果/已知事实完全一致(直接照抄,不要改写、不要补尾斜杠)。\n")
	builder.WriteString("6. 探测最多一次：结果为空或报错时不要换参数反复试探，直接用 need_more_info 说明缺什么。\n")
	builder.WriteString("7. 若本轮可用工具里只剩 " + IntentionToolStart + "（没有探测工具），不要再想探测，直接表态。\n")
	builder.WriteString("8. 信息不足时如实说明缺什么,不要用默认值填充,不要编造路径。\n")
	builder.WriteString("9. 本技能的完整说明见其 SKILL.md;下面的【技能判定说明】优先于你的直觉。\n")

	if guard := negotiateGuard(req.NegSpec); guard != pCommon.BlankString {
		builder.WriteString("\n 【技能判定说明(来自SKILL.md)】" + guard + "\n")
	}

	if req.Feedback != pCommon.BlankString {
		builder.WriteString("\n【上一次提交被校验器拒绝】\n" + req.Feedback + "\n")
		builder.WriteString("请修正后用 " + IntentionToolStart + " 重新提交。\n")
	}

	return builder.String()
}

func BuildUserPrompt(req NegotiationRequest) string {
	return "用户原话：" + strings.TrimSpace(req.Question)
}

func NeedMoreInfoText(result NegotiationResult) string {
	reason := strings.TrimSpace(result.Reason)

	missing := make([]string, 0, len(result.Missing))
	for _, item := range result.Missing {
		if trimmed := strings.TrimSpace(item); trimmed != pCommon.BlankString {
			missing = append(missing, trimmed)
		}
	}

	switch {
	case reason != pCommon.BlankString && len(missing) > 0:
		return reason + "\n请补充：" + strings.Join(missing, "\n- ")
	case reason != pCommon.BlankString:
		return reason
	case len(missing) > 0:
		return "需要补充：" + strings.Join(missing, "\n- ")
	default:
		return "需要补充更多信息才能开始..."
	}
}

func GetTaskLabel(task PlanTask) string {
	if task.Section != pCommon.BlankString {
		return task.Section
	}

	if task.Action != pCommon.BlankString {
		return task.Action
	}

	return task.ID
}

// GetNegotiationReadPath 根据原始材料目录、目标探测的目录得到系统工具读取的目录列表
// detectedPath 必须在 materialsRoot目录范围内
func GetNegotiationReadPath(materialsRoot, detectedPath string) []string {
	roots := make([]string, 0, 2)

	root := strings.TrimSpace(materialsRoot)
	if root != pCommon.BlankString {
		roots = append(roots, filepath.Clean(root))
	}

	dir := strings.TrimSpace(detectedPath)
	if dir == pCommon.BlankString || root == pCommon.BlankString {
		return roots
	}

	info, err := os.Stat(dir)
	if err != nil {
		return roots // 不存在：不需要探测
	}

	if !info.IsDir() {
		dir = filepath.Dir(dir)
	}

	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(dir))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return roots // 目录越权
	}

	roots = append(roots, filepath.Clean(dir))

	return roots
}

func IsIntentionStart(toolName string) bool {
	return strings.EqualFold(strings.TrimSpace(toolName), IntentionToolStart)
}

// 从协商请求中将系统工具名称列表取出来
func restoreSystemToolNames(req NegotiationRequest) []string {
	names := make([]string, 0, len(req.SystemTools))
	seen := make(map[string]struct{}, len(req.SystemTools))

	for _, tool := range req.SystemTools {
		name := strings.TrimSpace(tool.Function.Name)
		if name == pCommon.BlankString || name == IntentionToolStart {
			continue
		}

		if _, dup := seen[name]; dup {
			continue
		}

		names = append(names, name)
		seen[name] = struct{}{}
	}
	sort.Strings(names)
	return names
}
