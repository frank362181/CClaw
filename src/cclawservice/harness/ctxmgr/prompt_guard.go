/*
* 对发送给LLM的prompt的上下文的顺序进行约定
 */

package ctxmgr

import (
	"strings"

	hCommon "my.favorite/cclawservice/harness/common"
)

// 稳定段的规范顺序(标题),既是文档、也是测试断言依据
// 顺序是"前缀缓存"的前提:人工文件 -> 配置清单;任何"随轮次变化"的段都不得进system
var promptStableSectionOrder = []string{
	hCommon.AgentDocFileName,
	hCommon.SoulDocFileName,
	hCommon.IdentityDocFileName,
	hCommon.UserDocFileName,
	hCommon.ToolsDocFileName,
	hCommon.BootstrapDocFileName,
	hCommon.MemoryDocFileName, // 人工区

	"Available Skills",
	"Available Tools",
}

// 在system prompt中不允许出现的易变标记
// 如果出现,说明有人回写了,导致标记之后的内容缓存全部作废
var volatileSystemMarkers = []struct{ Marker, Reason string }{
	{"## 当前时间", "每轮变化:应随 TurnContext.Now 后置"},
	{"## 历史摘要", "压缩/合并时变化:应随 TurnContext.Summary 后置"},
	{sectionTitlePrefix + "Skill:", "随意图门变化:应随 TurnContext.Skills后置"},
	{hCommon.MemoryAutoBeginPrefix, "蒸馏时变化:MEMORY.md 自动区后置"},
}

func ValidateStablePrompt(prompt string) []string {
	var violations []string
	for _, item := range volatileSystemMarkers {
		if strings.Contains(prompt, item.Marker) {
			violations = append(violations, item.Marker+"<-"+item.Reason)
		}
	}

	return violations
}
