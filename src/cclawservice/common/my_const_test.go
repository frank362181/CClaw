/**
* 前后端共享的 SSE 载荷取值契约测试。
*
* 这些字符串由后端下发给 cclawchat,前端按**值**分支渲染(工具卡片状态、收尾提示样式),
* 一旦改名会静默降级为"未知状态"(例如工具失败被显示成"等待执行"),因此在这里锁死。
*
* 注意:仅在前后端同时改名时才能同步修改本测试。
 */

package common

import "testing"

// TestToolEventPhaseContract 工具调用阶段值。
// 前端映射见 cclawchat/scripts/modules/chat.js 的 phases(以及 _renderMessage 里的 toolLog 渲染)。
func TestToolEventPhaseContract(t *testing.T) {
	cases := []struct {
		got  string
		want string
		desc string
	}{
		{ChatToolEventIntent, "intent", "模型声明将调用工具"},
		{ChatToolEventRunning, "running", "开始执行"},
		{ChatToolEventProgress, "progress", "执行过程输出"},
		{ChatToolEventDone, "done", "执行成功"},
		{ChatToolEventError, "error", "执行失败"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("工具阶段值漂移(%s): %q, 期望 %q;前端将无法正确渲染该状态", c.desc, c.got, c.want)
		}
	}
}

// TestChatNoticeCodeContract 流式收尾提示 code。
// 前端按 code 去重并按 .stream-notice[data-code] 渲染,见 chat.js/state.js。
func TestChatNoticeCodeContract(t *testing.T) {
	cases := []struct {
		got  string
		want string
	}{
		{ChatNoticeTruncated, "truncated"},
		{ChatNoticeContentFilter, "content_filter"},
		{ChatNoticeIncomplete, "incomplete"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("收尾提示 code 漂移: %q, 期望 %q", c.got, c.want)
		}
	}
}
