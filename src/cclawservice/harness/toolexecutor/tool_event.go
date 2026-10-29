/**
* 工具执行的 **业务事件出口**
*
* 事件类型与"谁能收事件"的最小契约都定义在执行层:
* 否则执行器就必须依赖编排层的门面接口(chat.ISink)才能发一个进度事件。
**/

package toolexecutor

// ToolEvent 一次工具调用的业务事件(下发给前端做状态渲染)。
// Phase 取值见 cclawservice/common 的 ChatToolEvent*(intent/running/progress/done/error)。
type ToolEvent struct {
	Phase  string
	Seq    int
	Name   string
	Detail string
}

// ToolEventSink 执行器对 sink 的 **最小** 要求:只要能收工具事件。
// chat.ISink 天然满足(它有 OnTool(ToolEvent)),因此两个包之间不需要互相引用。
type ToolEventSink interface {
	OnTool(event ToolEvent)
}
