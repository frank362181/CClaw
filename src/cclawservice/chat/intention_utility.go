/**
* plan mode场景下定义的工具函数
**/

package chat

import (
	"fmt"
	"strings"

	"my.favorite/cclawservice/chat/sop"
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

func NeedMoreInfoText(missing []string) string {
	if len(missing) == 0 {
		return "需要补充更多信息才能开始..."
	}

	return "需要补充:" + strings.Join(missing, "、")
}

func GetTaskLabel(task sop.PlanTask) string {
	if task.Section != pCommon.BlankString {
		return task.Section
	}

	if task.Action != pCommon.BlankString {
		return task.Action
	}

	return task.ID
}
