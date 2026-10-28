/*
* 降级器,单独设计的目的:
* 	降级是 "改写请求"的副作用,不是重试策略
*	保证调用一次降一级
**/

package api

import (
	"my.favorite/cclawservice/harness/api/llmproto"
	"my.favorite/cclawservice/harness/ctxmgr"
)

type Degrader struct{ done bool }

func NewDegrader() *Degrader {
	return &Degrader{}
}

func (d *Degrader) apply(req *llmproto.ChatCompletionRequest, err error) bool {
	if d.done {
		return false
	}

	if !ctxmgr.DegradeChatRequest(req, err) {
		return false
	}

	d.done = true
	return true
}
