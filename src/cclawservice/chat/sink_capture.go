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
	"time"
	"unicode/utf8"

	pCommon "my.favorite/platformutility/common"
)

type CapturingSink struct {
	ISink

	mu sync.Mutex
	sb strings.Builder

	// 首个"内容类"事件时刻(正文 / 思考 / 工具)
	firstAt time.Time
}

func NewCapturingSink(s ISink) *CapturingSink {
	if s == nil {
		s = NopSink{}
	}

	return &CapturingSink{ISink: s}
}

func (c *CapturingSink) markVisible() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.firstAt.IsZero() {
		c.firstAt = pCommon.GetDatetimeUtilityInstance().GetNow()
	}
}

func (c *CapturingSink) Streamed() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return strings.TrimSpace(c.sb.String())
}

func (c *CapturingSink) OnDelta(delta string) {
	c.markVisible()

	if delta != pCommon.BlankString {
		c.mu.Lock()
		c.sb.WriteString(delta)
		c.mu.Unlock()
	}

	c.ISink.OnDelta(delta)
}

func (c *CapturingSink) OnReasoning(delta string) {
	c.markVisible()
	c.ISink.OnReasoning(delta)
}

func (c *CapturingSink) OnTool(event ToolEvent) {
	c.markVisible()
	c.ISink.OnTool(event)
}

func (c *CapturingSink) Runes() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return utf8.RuneCountInString(strings.TrimSpace(c.sb.String()))
}

// FirstVisibleMs 回合开始 --> 首个可见事件的毫秒数;从未发生返回 0
func (c *CapturingSink) FirstVisibleMs(start time.Time) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.firstAt.IsZero() {
		return 0
	}

	return c.firstAt.Sub(start).Milliseconds()
}

// 编译期断言
var _ IStreamedSink = (*CapturingSink)(nil)
