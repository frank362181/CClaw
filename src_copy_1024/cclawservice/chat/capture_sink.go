/*
* 记录用户实际看到的正文,用于中断/失败时的历史补写
*
*【为什么必须存在】
*	- 一次提问可能经过多轮工具循环,每轮都是一次独立的Stream:
*		- round0:"我先读一下配置" <- 用户已经看到
*		- round1:"这个配置里..." <- 用户也看到了
*		- round2:被用户中断了
*
*	LlmReply只是承载最后一轮,只有把每一轮的OnDelta累加起来,才是用户实际看到的
*
*【不需要捕获OnReasoning】
*	思考过程不是回答:写进assistant机会会让下一轮模型把自己上一轮的推理结果当成
*	已经确定的结论继续往下推理,也会让历史体积因思考内容暴涨
**/

package chat

import (
	"strings"
	"sync"
	"unicode/utf8"

	pCommon "my.favorite/platformutility/common"
)

type capturingSink struct {
	ISink

	mu sync.Mutex
	sb strings.Builder
}

func newCapturingSink(s ISink) *capturingSink {
	if s == nil {
		s = NopSink{}
	}

	return &capturingSink{ISink: s}
}

func (c *capturingSink) Streamed() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return strings.TrimSpace(c.sb.String())
}

func (c *capturingSink) OnDelta(delta string) {
	if delta != pCommon.BlankString {
		c.mu.Lock()
		c.sb.WriteString(delta)
		c.mu.Unlock()
	}

	c.ISink.OnDelta(delta)
}

func (c *capturingSink) OnReasoning(delta string) {}

func (c *capturingSink) Runes() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return utf8.RuneCountInString(strings.TrimSpace(c.sb.String()))
}

// 编译期断言
var _ IStreamedSink = (*capturingSink)(nil)
