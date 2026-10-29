/**
* 一轮对话状态的生命周期管理
**/

package chat

import (
	"fmt"

	"my.favorite/cclawservice/common/llmproto"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/toolexecutor"
	"my.favorite/cclawservice/model/dataobject"
)

// TurnState 一轮对话的状态,生命周期 == 一次service.execute
type TurnState struct {
	Cmd   Command
	Agent dataobject.TAgent
	//AgentRef AgentReference
	Provider Provider

	SystemPrompt       hCommon.SystemPrompt
	SystemPromptString string
	VisibleTools       []llmproto.Tool
	Plan               IRoundPlanner
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

func (t *TurnState) AppendToolResult(callId string, body string) {
	t.messages = append(t.messages, buildToolMessage(callId, body))
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

func (t *TurnState) Env() toolexecutor.ToolEnv {
	return toolexecutor.ToolEnv{
		Resolver: t.Scripts,
		WorkRoot: t.WorkRoot,
	}
}

func (t *TurnState) Outcomes() []toolexecutor.ToolOutcome {
	return t.outcomes
}
