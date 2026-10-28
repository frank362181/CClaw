/*
* 降级器,单独设计的目的:
* 	降级是 "改写请求"的副作用,不是重试策略
*	保证调用一次降一级
**/

package chat

import (
	"strings"

	"my.favorite/cclawservice/common/llmproto"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
)

type Degrader struct{ done bool }

func NewDegrader() *Degrader {
	return &Degrader{}
}

func (d *Degrader) apply(req *llmproto.ChatCompletionRequest, err error) bool {
	if d.done {
		return false
	}

	if !degradeChatRequest(req, err) {
		return false
	}

	d.done = true
	return true
}

// degradeChatRequest 错误信息中包含的信息对chat request降级
func degradeChatRequest(req *llmproto.ChatCompletionRequest, err error) bool {
	if req == nil || err == nil {
		return false
	}

	if req.ToolChoice == pCommon.BlankString &&
		req.EnableThinking == nil &&
		req.StreamOptions == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "tool_choice") &&
		!strings.Contains(msg, "enable_thinking") &&
		!strings.Contains(msg, "stream_options") &&
		!strings.Contains(msg, "include_usage") {
		return false
	}

	logging.LogWarnF("chat degrade: provider rejected tool forced-call params (clear tool_choice=%s/enable_thinking/stream_options), retry: %s",
		req.ToolChoice, err.Error())

	//对强制工具调用进行降级
	req.ToolChoice = pCommon.BlankString
	req.EnableThinking = nil
	req.StreamOptions = nil //代价:LLM供应商流式拿不到usage,将退化为"不可观测",而非报错

	return true
}
