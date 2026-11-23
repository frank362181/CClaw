/**
* plan mode场景下定义的工具函数
**/

package plan_mode

import (
	"fmt"
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

	builder.WriteString("\n【可用工具】只有 " + IntentionToolFsList + " / " + IntentionToolFsStat +
		":只读,返回文件名、大小与扩展名统计,不返回文件内容。\n")

	// 只保留机制性规则:可被校验器/schema 强化;业务判据走 IntentGuard
	builder.WriteString("\n【机制规则】\n")
	builder.WriteString("1. 必须调用 " + IntentionToolStart + " 表态,不要用普通文本回答。\n")
	builder.WriteString("2. 参数必须真实存在:不确定就用 " + IntentionToolFsList + "/" +
		IntentionToolFsStat + " 验证;**不要编造路径**。\n")
	builder.WriteString("3. 信息不足时如实说明缺什么,不要用默认值填充。\n")
	builder.WriteString("4. 本技能的完整说明见其 SKILL.md;下面的【技能判定说明】优先于你的直觉。\n")

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

func NeedMoreInfoText(missing []string) string {
	if len(missing) == 0 {
		return "需要补充更多信息才能开始..."
	}

	return "需要补充:" + strings.Join(missing, "、")
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
