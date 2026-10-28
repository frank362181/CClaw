/*
* 面向 LLM的领域模型和参数对象
* 	- 不同 LLM 可能会因为 LLM的不同参数可能不同
**/

package chat

import (
	"fmt"
	"unicode/utf8"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/harness/api/llmproto"
	pCommon "my.favorite/platformutility/common"
)

// LlmRequest 一次上游调用的完整参数(parameter object)
// 取代 Complete / CompleteWithTools / ChatCompleteMessages等函数的签名
type LlmRequest struct {
	Messages   []llmproto.ChatMessage
	Tools      []llmproto.Tool
	ToolChoice string // llmproto.ToolChoice*;空=auto
	Streamed   bool
	MaxTokens  int // <= 0:由ctxmgr按context_window动态推导
}

// LlmReply 一次上游调用的返回(值对象)
// 取代StreamWithTools 返回的 4个值,现在仅一个
type LlmReply struct {
	Content      string
	ToolCalls    []llmproto.ToolCall
	FinishReason string
}

// ToolOutcome 一次工具调用的结果值对象
// [关键不变量] 将两条互相冲突的消息口径封装在类型里,调用点不需要自己决定怎么截断:
//   - ForModel: 返回给模型 --- 截断到maxRunes(总长含省略号)
//   - ForRecord: 写历史文件 --- 完整原文(DB的 256 rune截断由journal负责)
type ToolOutcome struct {
	Seq    int
	CallID string
	Name   string
	Arg    string

	Full   string // 工具调用的完整原始输出
	Reason error  // 非nil表示执行失败

	CallAt, RespAt int64
	ElapseMs       int64 // 工具执行耗费的时间,毫秒
}

func (o ToolOutcome) Failed() bool { return o.Reason != nil }

// ForRecord 返回写入历史/旁路记录的文本:完整/截断
func (o ToolOutcome) ForRecord() string {
	if o.Reason != nil {
		return fmt.Sprintf(`{"error":%q}`, o.Reason.Error())
	}

	return o.Full
}

// ForModel 返回回填给模型的文本:截断到maxRunes(省略号计入总数)
func (o ToolOutcome) ForModel(maxRunes int) string {
	marker := cCommon.TruncatedMarker
	m := utf8.RuneCountInString(marker)
	if maxRunes <= 0 || m >= maxRunes {
		return pCommon.TruncateRunes(o.ForRecord(), maxRunes, marker)
	}

	return pCommon.TruncateRunes(o.ForRecord(), maxRunes-m+1, marker)
}

// Reply 用例输出,不带任何 HTTP/DB 语义
type Reply struct {
	SessionID    string
	TurnID       string
	Content      string
	FinishReason string
	At           int64
}
