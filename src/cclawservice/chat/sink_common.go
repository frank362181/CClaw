/**
* 事件出口(Port):上行增量与业务结果从这里离开 chat 包
*
* 两个层次,不要混用:
*   - IStreamObserver: **传输级**,只关心模型侧增量(同步路径注入 NopObserver)
*   - ISink          : **业务级**,在增量之上补工具/提示/记账/结束原因(同步路径注入 NopSink)
**/

package chat

import "my.favorite/cclawservice/harness/toolexecutor"

// IStreamObserver 上游增量的 **传输级** 出口,同步路径注入 NopObserver
type IStreamObserver interface {
	OnDelta(delta string)
	OnReasoning(delta string)
	OnToolIntent(index int, name string)
}

// NopObserver 空对象:让同步路径无需if-nil判断,也让"已输出即不重试"的判定天然为 false
type NopObserver struct{}

func (NopObserver) OnDelta(delta string)                {}
func (NopObserver) OnReasoning(delta string)            {}
func (NopObserver) OnToolIntent(index int, name string) {}

// ======================= 一轮对话的 **业务事件出口** =============================

// ISink 由于同时满足 IStreamObserver,因此可以直接传给 ILlmGateway.Stream
// 这是同步/异步编排的关键:同步路径注入NopSink
type ISink interface {
	IStreamObserver

	IsStreamed() bool //是否流式

	OnStatus(code, text string)
	OnTool(event toolexecutor.ToolEvent)
	OnNotice(code, message string)
	OnJournal(err error)
	OnFinishReason(reason string)
}

// IStreamedSink 能读出已经下发给用户的内容的Sink
// 单独成接口,而不是直接塞入到ISink:同步路径的 NopSink 没有内容可读
type IStreamedSink interface {
	ISink

	// Streamed 返回截止当前已通过OnDelta下发的正文(不包含思考过程)
	Streamed() string
}
