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

	"my.favorite/cclawservice/harness/api/llmproto"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/request"
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

// AgentReference 是跨横切端口所必须的最小Agent身份
// 端口契约要稳定,若直接将TAgent给传递进来,将来数据库字段变更,这里很可能也要变更
type AgentReference struct {
	ID   string
	Name string // 仅仅用于日志

	MaxConcurrence int // 表示agent的并发数,<= 0 表示未配置,用默认值来代替
}

func NewAgentReference(agent dataobject.TAgent) *AgentReference {
	return &AgentReference{
		ID:             agent.AgentId,
		Name:           agent.AgentName,
		MaxConcurrence: agent.MaxConcurrents,
	}
}

func (r *AgentReference) IsZero() bool {
	return len(strings.TrimSpace(r.ID)) == 0
}

// HistoryAgentReference 会话历史 **不透明**存储标识
type HistoryAgentReference string

func (h HistoryAgentReference) IsZero() bool {
	return len(h) == 0
}

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

// ITransport 上游 LLM 调用
// "带不带工具"、"消息从哪里来"全部由LlmRequest表达,不需要再增加方法
type ITransport interface {
	Complete(ctx context.Context, p Provider, req LlmRequest) (LlmReply, error)
	Stream(ctx context.Context, p Provider, req LlmRequest, ob IStreamObserver) (LlmReply, error)
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
	HistoryRef HistoryAgentReference
	Recorded   bool // false--未启用记账 / 归属冲突 / 建档失败:本轮只回话,不记账
}

func (s Session) Valid() bool {
	return !s.Recorded || strings.TrimSpace(s.ID) != pCommon.BlankString
}

func (s Session) Recordable() bool {
	return s.Recorded && strings.TrimSpace(s.ID) != pCommon.BlankString
}

func (s Session) HasHistory() bool {
	return s.Recorded && !s.HistoryRef.IsZero()
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

// IConcurrencyGate 并发闸
type IConcurrencyGate interface {
	// Acquire 获取 agent的并发许可
	// 返回的release必须支持幂等,便于defer
	Acquire(ctx context.Context, agentRef AgentReference) (release func(), err error)
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
	NewCommand(comCode, sender string, chatReq *request.ChatRequest) (*Command, error)
	Prepare(ctx context.Context, cmd Command) (*TurnState, error)
}

// ITurnReporter 可观测性
// 原先 WithTurn / OnTurnTiming / MilliDiff等
type ITurnReporter interface {
	Begin(ctx context.Context, c Command) (context.Context, *TurnTiming)
	End(ctx context.Context, t *TurnTiming, firstVisibleMs int64, r Reply, err error, startAt time.Time)
}

// ------------------------ 轮次状态--------------------

// TurnState 一轮对话的状态,生命周期 == 一次service.execute
type TurnState struct {
	Cmd      Command
	Agent    dataobject.TAgent
	AgentRef AgentReference
	Provider Provider

	SystemPrompt       hCommon.SystemPrompt
	SystemPromptString string
	VisibleTools       []llmproto.Tool
	Plan               IRoundPlanner
	Policy             hCommon.ContextPolicy // 完整预算策略
	ToolsMaxRunes      int                   //单条工具结果回填给模型的上限

	Scripts  IScriptsResolver
	WorkRoot string // agent的工作区根目录

	Timing *TurnTiming
	Ledger IMemoryLedger

	Session Session

	Streamed bool //本轮是否为流式
	Scope    string

	// 可变:消息序列 + 工具结果
	messages []llmproto.ChatMessage
	outcomes []ToolOutcome
}

// Request 构造本轮的一次上游请求
func (t *TurnState) Request(tools []llmproto.Tool, choice string) LlmRequest {
	return LlmRequest{
		Messages:   t.messages, // 切片头拷贝,后续append不会影响已传入的请求
		Tools:      tools,
		ToolChoice: choice,
		Streamed:   t.Streamed,
	}
}

// Seed 注入装配好的初始消息序列(system + 历史 + 本轮用户消息)
func (t *TurnState) Seed(messages []llmproto.ChatMessage) {
	t.messages = make([]llmproto.ChatMessage, len(messages))
	copy(t.messages, messages)
}

func (t *TurnState) Messages() []llmproto.ChatMessage {
	return t.messages
}

func (t *TurnState) AppendAssistant(r LlmReply) {
	t.messages = append(t.messages, buildAssistantMessage(r.Content, r.ToolCalls))
}

func (t *TurnState) AppendToolResult(callId string, body string) {
	t.messages = append(t.messages, buildToolMessage(callId, body))
}

// RecordOutcomes 收集本轮工具结果(收尾时交给 Journal落库)
func (t *TurnState) RecordOutcomes(outcomes []ToolOutcome) {
	t.outcomes = append(t.outcomes, outcomes...)
}

func (t *TurnState) IsStreamed() bool {
	return t.Streamed
}

func (t *TurnState) SetStreamed(isStreamed bool) {
	t.Streamed = isStreamed
}

func (t *TurnState) Scoped() string {
	if t.Scope != "" {
		return t.Scope
	}

	t.Scope = fmt.Sprintf("agent=%s,turn=%s", t.AgentRef.ID, t.Cmd.TurnID)
	return t.Scope
}

func (t *TurnState) Env() ToolEnv {
	return ToolEnv{
		Resolver: t.Scripts,
		WorkRoot: t.WorkRoot,
	}
}

func (t *TurnState) Outcomes() []ToolOutcome {
	return t.outcomes
}

// ToolScriptArg 工具脚本执行的参数
type ToolScriptArg struct {
	Path     string // 脚本绝对路径
	Args     string //模型给出的JSON参数,原样作为脚本入参
	WorkRoot string // 脚本的工作目录

	// 非nil表示调用方消费进度:实现应走流式执行,逐行回调 stdout + stderr;
	// nil表示无消费者,实现可走缓冲路径(省掉逐行回调与管道编排的开销)
	OnLine func(line string)
}

// IToolScriptRunner 执行一个工具脚本
// 只暴露一个 **方法
type IToolScriptRunner interface {
	Run(ctx context.Context, toolArgs ToolScriptArg) (string, error)
}

// ----------------------- 计时------------------

// TurnTiming 本轮 **自增**的计数器
type TurnTiming struct {
	ConcurrencyMs int64 // Agent并发闸等待
	RouteMs       int64 // 路由(会话绑定/ 关键词 / LLM分类)
	SkillMs       int64 // 技能读盘 + 工具合成
	PromptMs      int64 // 意图门 + system prompt + 首轮计划

	LlmMs     int64 // 所有上游调用耗时之和
	ToolsMs   int64 // 所有工具调用之和
	Rounds    int   // 上游调用轮次
	ToolCalls int   // 工具调用次数
}
