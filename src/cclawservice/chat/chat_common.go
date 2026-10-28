/*
* 全局通用的聊天相关的类型定义,不同于model.go中的定义：
*	- model.go中定义的与 LLM 厂家协议相关,port.go中定义在创建后将不会改变
**/

package chat

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/common/llmproto"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/ctxmgr"
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

// IRoundPlanner 决定 第 几 round轮实际下发哪些工具、是否强制调用
// 取代 直接依赖的tool.FirstRoundToolPlan
type IRoundPlanner interface {
	// RoundTools 第 round 轮实际下发的工具与tool choice
	RoundTools(all []llmproto.Tool, round int) (tools []llmproto.Tool, toolChoice string)

	// Verify 哨兵:校验模型是否遵守本轮强制调用(tool_choice=required)
	Verify(calls []llmproto.ToolCall, toolChoice string)
}

// IScriptsResolver 工具名 -> 工具脚本绝对函数的解析
type IScriptsResolver interface {
	Resolve(toolName string) (string, error)
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

// Record 一条会话历史记录的值对象
// 维护流程(compact/merge) 需要 **正文**
type Record struct {
	Index      int
	Kind       string // user / assistant / tool / summary ...
	Title      string
	TimeText   string // 历史文件中只有文本类的时间
	Content    string
	CoversFrom int // kind = summary时有效;0 表示不覆盖
	CoversTo   int
}
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

// IToolExecutor 工具执行策略:同步接口串行、流式接口同轮并行 + 进度汇集
// 这是 runToolLoop和streamToolLoop之间的唯一的差异
type IToolExecutor interface {
	// RunAll 执行本轮模型声明的全部工具调用
	// 契约:
	// 	- 返回的outcome **顺序**必须与 calls 一致
	//  - 返回条数 <= len(calls):只有 ctx 已取消时才能缺条
	RunAll(ctx context.Context, env ToolEnv, invocations []ToolInvocation, sink ISink, ledger IMemoryLedger) []ToolOutcome
}

// ======================= 一轮对话的 **业务事件出口** =============================

// ToolEvent 业务层工具事件
type ToolEvent struct {
	Phase  string
	Seq    int
	Name   string
	Detail string
}

// ISink 由于同时满足 IStreamObserver,因此可以直接传给 ILlmGateway.Stream
// 这是同步/异步编排的关键:同步路径注入NopSink
type ISink interface {
	IStreamObserver

	IsStreamed() bool //是否流式

	OnStatus(code, text string)
	OnTool(event ToolEvent)
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

// Command 是聊天用例的 **输入值对象**
// 与request.ChatRequest的区别:
//   - ChatRequest 是 **线协议 DTO**,字段克被客户端伪造,只在 controller里存在
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

// ITurnRegistrator 中断登记
// 替换 ctxmgr.GetTurnStreamMgr() 这个全局单例
// 测试时可注入 内存实现:键的构造规则也收回接口实现内部(不再散落在Service)
type ITurnRegistrator interface {
	Register(ctx context.Context, cmd Command) (context.Context, func())
	Cancel(sender, turnID string) bool
}

// ITurnPreparer 把Command转换成本轮可执行的TurnState
// 路由 -> Provider -> 技能 -> system prompt -> 工具/预算 -> Plan
type ITurnPreparer interface {
	Prepare(ctx context.Context, cmd Command) (*TurnState, error)
}

// ITurnReporter 可观测性
// 原先 WithTurn / OnTurnTiming / MilliDiff等
type ITurnReporter interface {
	Begin(ctx context.Context, c Command) (context.Context, *TurnTiming)
	End(ctx context.Context, t *TurnTiming, firstVisibleMs int64, r Reply, err error, startAt time.Time)
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

// ----------------------- 计时------------------

// TurnTiming 本轮 **自增**的计数器
type TurnTiming struct {
	TurnScope string //归因标识(turnID); 由TurnReporter.Begin填写
	AgentId   string // 目标Agent;由Preparer.Prepare在路由完成后填写

	ConcurrencyMs int64 // Agent并发闸等待
	RouteMs       int64 // 路由(会话绑定/ 关键词 / LLM分类)
	SkillMs       int64 // 技能读盘 + 工具合成
	PromptMs      int64 // 意图门 + system prompt + 首轮计划

	LlmMs     int64 // 所有上游调用耗时之和
	ToolsMs   int64 // 所有工具调用之和
	Rounds    int   // 上游调用轮次
	ToolCalls int   // 工具调用次数
}
