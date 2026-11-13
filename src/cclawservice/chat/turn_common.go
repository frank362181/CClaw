/**
* 一轮对话状态的生命周期管理
**/

package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"my.favorite/cclawservice/common/llmproto"
	selfException "my.favorite/cclawservice/exception"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/toolexecutor"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

// TurnRole 定义每轮扮演的角色
type TurnRole int

const (
	TurnRoleRoot      TurnRole = iota + 1 // 入口轮
	TurnRoleGenerator                     // 子任务:目标Agent由计划给定
	TurnRoleEvaluator                     // 统一评估:目标Agent由pickEvaluator给定
)

func (t TurnRole) String() string {
	switch t {
	case TurnRoleGenerator:
		return "Generator"
	case TurnRoleEvaluator:
		return "Evaluator"
	default:
		return "Root"
	}
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
	PrepareFor(ctx context.Context, cmd Command, agent dataobject.TAgent, turnRole TurnRole) (*TurnState, error)
}

// ITurnReporter 可观测性
// 原先 WithTurn / OnTurnTiming / MilliDiff等
type ITurnReporter interface {
	Begin(ctx context.Context, c Command) (context.Context, *TurnTiming)
	End(ctx context.Context, t *TurnTiming, firstVisibleMs int64, r Reply, err error, startAt time.Time)
}

// TurnState 一轮对话的状态,生命周期 == 一次service.execute
type TurnState struct {
	Cmd      Command
	Agent    dataobject.TAgent
	Provider Provider

	SystemPrompt       hCommon.SystemPrompt
	SystemPromptString string
	VisibleTools       []llmproto.Tool
	TurnTool           IRoundTool
	Policy             hCommon.ContextPolicy // 完整预算策略
	ToolsMaxRunes      int                   //单条工具结果回填给模型的上限

	Scripts  toolexecutor.IScriptsResolver
	WorkRoot string // agent的工作区根目录

	Timing *TurnTiming
	Ledger IMemoryLedger

	Session Session

	Streamed bool //本轮是否为流式
	Scope    string

	// 可变:消息序列 + 工具结果
	messages []llmproto.ChatMessage
	outcomes []toolexecutor.ToolOutcome
}

// Request 构造本轮的一次上游请求
func (t *TurnState) Request(tools []llmproto.Tool, choice string) llmproto.LlmRequest {
	return llmproto.LlmRequest{
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

func (t *TurnState) AppendAssistant(r llmproto.LlmReply) {
	t.messages = append(t.messages, buildAssistantMessage(r.Content, r.ToolCalls))
}

func (t *TurnState) AppendToolResult(callId string, body string) error {
	if strings.TrimSpace(callId) == pCommon.BlankString {
		return selfException.ToolExecutionMisMatchedKind.Newf("tool result without call id: scope=%s, tools=%d, body=%s",
			t.Scoped(), len(t.VisibleTools), pCommon.TruncateRunes(body, 200, "..."),
		)
	}
	t.messages = append(t.messages, buildToolMessage(callId, body))
	return nil
}

// RecordOutcomes 收集本轮工具结果(收尾时交给 Journal落库)
func (t *TurnState) RecordOutcomes(outcomes []toolexecutor.ToolOutcome) {
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

	t.Scope = fmt.Sprintf("agent=%s,turn=%s", t.Agent.AgentId, t.Cmd.TurnID)
	return t.Scope
}

func (t *TurnState) NewToolEnv(rules toolexecutor.ToolConfigRules) toolexecutor.ToolEnv {
	return toolexecutor.NewToolEnv(t.Scripts, t.WorkRoot, t.Scoped(), rules)
}

func (t *TurnState) Outcomes() []toolexecutor.ToolOutcome {
	return t.outcomes
}

// TurnRequest 一轮对话的输入
type TurnRequest struct {
	Cmd  Command
	Turn *TurnState // 必须有Prepare 构造完成
	Role TurnRole
	Sink ISink

	// 是否允许写历史
	// 最终以journal.Begin返回的Session.Recordable()为准
	AllowRecord bool

	DisableTool bool

	ParentTurnID string
	StartAt      time.Time //计时起点,调用方传入
}

// TurnOutcome 一轮聊天的执行结果
type TurnOutcome struct {
	TurnReply      Reply
	Llm            llmproto.LlmReply
	Rounds         int // 上游调用轮次
	ToolCalls      int //工具调用次数
	ElapseMs       int64
	FirstVisibleMs int64
}

type ITurnRunner interface {
	Run(ctx context.Context, req TurnRequest) (TurnOutcome, error)
}

// -----------------------每轮聊天计时------------------

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
