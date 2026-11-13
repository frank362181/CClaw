/**
* 一轮对话的 **值对象与用例接口**
*
* 只放"数据长什么样"和"用例需要什么能力",不放任何实现:
*   - 值对象: Command / Provider / Session / Record / Assembled / Reply / TurnTiming
*   - 用例接口: Transport / Plan / Assemble / Journal / Ledger / Guard / Maintainer / Registrator / Preparer / Reporter
*
* 与 model.go 的分工:model.go 描述 LLM 厂家协议,本文件描述本进程内的业务契约。
**/

package chat

import (
	"context"
	"strings"

	"my.favorite/cclawservice/common/llmproto"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/harness/toolexecutor"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

// Provider 是一次绑定好的上游LLM端点(配置+能力)
type Provider struct {
	Llm dataobject.TLlm
}

func (p Provider) Usable() bool          { return len(strings.TrimSpace(p.Llm.ProviderId)) > 0 }
func (p Provider) ContextWindow() uint32 { return p.Llm.ContextWindow }
func (p Provider) MaxOutput() int {
	return p.Llm.MaxTokens
}
func (p Provider) Identifier() string { return p.Llm.ProviderId }

// Reply 用例输出,不带任何 HTTP/DB 语义,对LlmReply进行归一化
type Reply struct {
	SessionID    string
	TurnID       string
	Content      string
	FinishReason string
	At           int64
}

// ITransportClient 上游 LLM 调用
// "带不带工具"、"消息从哪里来"全部由LlmRequest表达,不需要再增加方法
type ITransportClient interface {
	SimpleComplete(ctx context.Context, llm dataobject.TLlm, system, user string, maxTokens int) (llmproto.LlmReply, error)
	Complete(ctx context.Context, p Provider, req llmproto.LlmRequest) (llmproto.LlmReply, error)
	Stream(ctx context.Context, p Provider, req llmproto.LlmRequest, ob IStreamObserver) (llmproto.LlmReply, error)
}

// IToolLooper 工具循环的可替换点
type IToolLooper interface {
	Run(ctx context.Context, st *TurnState, sink ISink, rules toolexecutor.ToolConfigRules) (llmproto.LlmReply, error)
}

// IRoundTool 决定 第 几 round轮实际下发哪些工具、是否强制调用
// 取代 直接依赖的tool.FirstRoundToolPlan
type IRoundTool interface {
	// RoundTools 第 round 轮实际下发的工具与tool choice
	RoundTools(all []llmproto.Tool, round int) (tools []llmproto.Tool, toolChoice string)

	// Verify 哨兵:校验模型是否遵守本轮强制调用(tool_choice=required)
	Verify(calls []llmproto.ToolCall, toolChoice string)
}

// IMemoryJournal 轮次记账(旁路、失败不阻断对话)
type IMemoryJournal interface {
	Begin(ctx context.Context, turnState *TurnState) (Session, error)
	Finish(ctx context.Context, turnState *TurnState, content string) int
	FinishPartial(ctx context.Context, turnState *TurnState, partial string, reason dataobject.InterruptReason) int
}

// IMemoryLedger 工具台账:两阶段留痕,执行前start,执行后Finish
// 用户中断时回填走不到这里 -- 只有执行者自己Finish,才不会丢"调用发生过的"证据
type IMemoryLedger interface {
	Start(name string, at int64) int
	Finish(idx int, at int64, result string)
	Snapshot() []ctxmgr.TurnToolRecord
}

// Assembled 一次上下文装配的产物:消息序列 + 预算信息 + 历史条目
type Assembled struct {
	Messages []llmproto.ChatMessage

	AssembleResult hCommon.AssembleResult //预算信息,供"是否超出窗口 -> 提示新建会话"判断,供维护触发条件使用
	Records        []Record
}

// IContextAssembler 装配上下文
type IContextAssembler interface {
	// Assemble 读回历史并按预算装配成本轮完整消息序列
	Assemble(ctx context.Context, turn *TurnState, userMessage string) (Assembled, error)

	// Fit 按预算裁剪消息序列(工具循环每轮调用)
	// 预算贴合与装配必须用 **同一份 policy**
	Fit(messages []llmproto.ChatMessage, policy hCommon.ContextPolicy) []llmproto.ChatMessage
}

// Session 一轮对话所属的会话 **值快照** (MemoryJournal.Begin)的产物
//   - History:为空表示新会话
type Session struct {
	ID         string
	AgentID    string
	Sender     string
	HistoryRef string
	Recorded   bool // false--未启用记账 / 归属冲突 / 建档失败:本轮只回话,不记账
}

func (s Session) Valid() bool {
	return !s.Recorded || strings.TrimSpace(s.ID) != pCommon.BlankString
}

func (s Session) Recordable() bool {
	return s.Recorded && strings.TrimSpace(s.ID) != pCommon.BlankString
}

func (s Session) HasHistory() bool {
	return s.Recorded && !(len(s.AgentID) == 0)
}

// ILoopGuard 一次对话内的工具调用死循环守护(每次都需新建)
// 禁止单例/或全局字段 -- 否则会跨轮次共享状态
type ILoopGuard interface {
	ShouldStop() bool
	ApplyLoopGuard(tool, args string, ok bool, result string, onNotice func(code, msg string)) string
	StopError(scope string) error
}

// Command 是聊天用例的 **输入值对象**
// 与request.ChatRequest的区别:
//   - ChatRequest 是 **线协议 DTO**,字段可被客户端伪造,只在 controller里存在
//   - Command 是 **规范化、已鉴权的业务输入**,构造后不可变
type Command struct {
	Sender      string
	ComCode     string
	SessionId   string
	TurnID      string
	Message     string // 已TrimSpace + 长度校验
	AgentID     string //可选:显式指定
	ChatType    int
	ChatChannel int
	GenAt       int64
}

// IMemoryMaintainer 记忆维护接口
type IMemoryMaintainer interface {
	Maintain(s Session, turnState *TurnState, asm Assembled, turnSeq int)
	Close(ctx context.Context, s Session, turnState *TurnState, reason int) error
}
