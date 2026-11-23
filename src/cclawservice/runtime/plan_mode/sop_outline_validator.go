/**
* SKILL.md 声明式流程(SOP)的校验,不同业务类型的SOP不同
*
* 采用"设计"与"执行"分离:校验是纯函数(无IO),可以在加载、单测、CI里反复跑
* 执行器只消费 **已经通过校验**的计划,不必到处防御
*
* 失败语义:校验不通过 -> 丢弃 Outline,技能退化为"单轮直答+工具循环"(今天的行为)
* 绝不能因为SOP写坏而让聊天不可用
**/

package plan_mode

import (
	"fmt"
	"sort"
	"strings"

	hCommon "my.favorite/cclawservice/harness/common"
	pCommon "my.favorite/platformutility/common"
)

// OutlineValidationIssue 表述一条检验问题
type OutlineValidationIssue struct {
	Rule    string
	StepID  string
	Message string
}

func (i OutlineValidationIssue) String() string {
	if i.StepID == pCommon.BlankString {
		return fmt.Sprintf("[%s] %s", i.Rule, i.Message)
	}

	return fmt.Sprintf("[%s] step=%s: %s", i.Rule, i.StepID, i.Message)
}

func HasNpaOutline(fm hCommon.SkillFrontMatter) bool {
	return len(fm.SOPOutLines) > 0
}

func OutlineToolNames(fm hCommon.SkillFrontMatter) map[string]struct{} {
	names := make(map[string]struct{}, len(fm.Tools))
	for _, tool := range fm.Tools {
		if name := strings.TrimSpace(tool.Name); len(name) > 0 {
			names[name] = struct{}{}
		}
	}

	return names
}

// ValidateNPAOutline 校验声明式流程,返回 稳定排序 的问题列表
// 规则:
//
//	L1 id 唯一非空;kind ∈ {tool,llm,check}
//	L2 可调度步骤的 needs 必须由**声明在前**的步骤产出(条件模板除外)
//	L3 kind=tool 的 tool 必须已在本技能 tools[] 注册
//	L4 kind=tool 必须有 produces;kind=check 必须有 checks
//	L5 至少一个 kind=llm(否则没有任何"写"的动作)
//	L6 至少存在一个终端产物(无人消费):对应 SOP 的"必须落盘交付"
//	L7 sections 非空 => contract.forbid-invent 必须为 true
//	L8 不允许在 args 里指定评审者(V3:评审者由引擎指派)
func ValidateNPAOutline(sopSteps []hCommon.SkillSOPStep, tools map[string]struct{}) []OutlineValidationIssue {
	issues := make([]OutlineValidationIssue, 0, 4)
	if len(sopSteps) == 0 {
		return []OutlineValidationIssue{{Rule: RuleNoLLMStep, Message: "SOP outline is nil"}}
	}

	// 第一遍:基础字段 + 产物索引
	indexById := make(map[string]int, len(sopSteps))     // 判断SOP中是否出现重复的ID
	procedureAt := make(map[string][]int, len(sopSteps)) // 产物名 -> 声明下标(含条件模版)
	hasLlm := false

	for idx, step := range sopSteps {
		stepID := strings.TrimSpace(step.ID)
		kind := strings.ToLower(strings.TrimSpace(step.Kind))

		// L1
		if stepID == pCommon.BlankString {
			issues = append(issues, OutlineValidationIssue{Rule: RuleDupID, Message: "id can not be nil or empty"})
		} else if _, dup := indexById[stepID]; dup {
			issues = append(issues, OutlineValidationIssue{Rule: RuleDupID, StepID: stepID, Message: "id can not be duplicate"})
		} else {
			indexById[stepID] = idx
		}

		if kind != hCommon.SOPStepKindTool && kind != hCommon.SOPStepKindLLM && kind != hCommon.SOPStepKindCheck {
			issues = append(issues, OutlineValidationIssue{Rule: RuleDupID, StepID: stepID, Message: fmt.Sprintf("kind illegal:%q(only tool/llm/check)", step.Kind)})
		}

		if kind == hCommon.SOPStepKindLLM {
			hasLlm = true
		}

		// L3
		if kind == hCommon.SOPStepKindTool {
			toolName := strings.TrimSpace(step.Tool)
			if toolName == pCommon.BlankString {
				issues = append(issues, OutlineValidationIssue{Rule: RuleMissingField, StepID: stepID, Message: "kind=tool,tool name must not be empty"})
			} else if _, ok := tools[toolName]; !ok {
				issues = append(issues, OutlineValidationIssue{Rule: RuleUnknownTool, StepID: stepID, Message: fmt.Sprintf("tool %s not registry", toolName)})
			}

			// L4
			if strings.TrimSpace(step.Produces) == pCommon.BlankString {
				issues = append(issues, OutlineValidationIssue{Rule: RuleMissingField, StepID: stepID, Message: "produces must not be empty"})
			}
		}

		// L4
		if kind == hCommon.SOPStepKindCheck && len(step.Checks) == 0 {
			issues = append(issues, OutlineValidationIssue{Rule: RuleMissingField, StepID: stepID, Message: "checks must not be empty"})
		}

		// L7
		if len(step.Sections) > 0 && (step.Contract == nil || !step.Contract.ForbidInvent) {
			issues = append(issues, OutlineValidationIssue{Rule: RuleContractAbsent, StepID: stepID,
				Message: fmt.Sprintf("The chapter-based expansion must declare \"contract.forbid-invent=true\"")})
		}

		// L8 args里不允许出现评审者
		for key := range step.Args {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "review") || strings.Contains(lower, "verify-agent") ||
				strings.Contains(lower, "verify_agent") {
				issues = append(issues, OutlineValidationIssue{Rule: RuleReviewerField, StepID: stepID,
					Message: fmt.Sprintf("args cannot dispatch reviewer %s", key)})
			}
		}

		if name := strings.TrimSpace(step.Produces); name != pCommon.BlankString {
			procedureAt[name] = append(procedureAt[name], idx)
		}
	}

	// L5
	if !hasLlm {
		issues = append(issues, OutlineValidationIssue{Rule: RuleNoLLMStep, Message: "must have a llm at least"})
	}

	//第二遍L2 依赖 + L6终端产物
	consumed := make(map[string]struct{}, len(procedureAt))
	for idx, step := range sopSteps {
		stepId := strings.TrimSpace(step.ID)
		conditional := strings.TrimSpace(step.When) != pCommon.BlankString

		for _, need := range step.Needs {
			need = strings.TrimSpace(need)
			if need == pCommon.BlankString {
				continue
			}

			consumed[need] = struct{}{}

			// 条件模版可自产自需
			if conditional && need == strings.TrimSpace(step.Produces) {
				continue
			}

			producers := procedureAt[need]
			declaredBefore := false
			for _, producerIdx := range producers {
				if producerIdx < idx {
					declaredBefore = true
					break
				}
			}

			if !declaredBefore {
				issues = append(issues, OutlineValidationIssue{Rule: RuleDanglingNeed, StepID: stepId,
					Message: fmt.Sprintf("referred the outcomes not declared before %s", need)})
			}
		}
	}

	// L6 必须有一个无人消费的产物(交付物)
	hasTerminalArtifact := false
	for name := range procedureAt {
		if _, ok := consumed[name]; !ok {
			// 无人消费 = 交付物
			hasTerminalArtifact = true
			break
		}
	}
	if !hasTerminalArtifact {
		issues = append(issues, OutlineValidationIssue{Rule: RuleNoDeliverable, Message: "no terminal artifact found"})
	}

	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Rule != issues[j].Rule {
			return issues[i].Rule < issues[j].Rule
		}

		return issues[i].StepID < issues[j].StepID
	})

	return issues
}
