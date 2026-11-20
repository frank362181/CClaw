/**
* 意图协商器:一个独立的针对用户问题的协商工具
*
* 设计意图:为何不塞进ToolLoop/TurnRunner
* - 协商阶段只允许只读工具 + 一个终止工具,和对话循环的工具集不同
* - 协商不写历史、不占并发、不需要装配器和记账
* - 独立实现,ToolLoop/TurnRunner 契约 0 改动
**/

package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/common/llmproto"
	"my.favorite/cclawservice/config"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
)

type IntentionNegotiator struct {
	transport ITransportClient
	config    config.ChatConfig
}

var _ INegotiator = (*IntentionNegotiator)(nil)

func NewIntentionNegotiator(transport ITransportClient, config config.ChatConfig) INegotiator {
	intentionNegotiator := &IntentionNegotiator{
		transport: transport,
		config:    config,
	}
	return intentionNegotiator
}

func (n *IntentionNegotiator) Negotiate(ctx context.Context, req NegotiationRequest) (NegotiationResult, error) {
	start := pCommon.GetDatetimeUtilityInstance().GetNow()
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = time.Duration(n.config.Negotiation.ArgTimeout) * time.Second
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	maxRounds := req.MaxRounds
	if maxRounds <= 0 {
		maxRounds = n.config.Negotiation.Rounds
	}
	if maxRounds < 1 {
		maxRounds = 1
	}

	messages := buildSimpleChatMessage(n.buildIntentionSystemPrompt(req), n.buildUserPrompt(req))
	negotiationResult := NegotiationResult{Decision: DecisionFailed}
	toolCalls := 0
	for round := 1; round <= maxRounds; round++ {
		if err := runCtx.Err(); err != nil {
			negotiationResult.Reason = "negotiate canceled:" + err.Error()
			return n.finish(negotiationResult, round, toolCalls, start), nil
		}

		last := round == maxRounds
		tools, choice := n.roundTools(req.SkillId, last)
		reply, err := n.transport.Complete(runCtx, req.Provider, llmproto.LlmRequest{
			Messages:        messages,
			Tools:           tools,
			ToolChoice:      choice,
			Streamed:        false,
			MaxTokens:       1024,
			DisableThinking: true, // 判定任务不需要思考链:省token、降延迟、必考与tool_choice的互斥
		})
		if err != nil {
			negotiationResult.Reason = "negotiate upcall failed:" + err.Error()
			return n.finish(negotiationResult, round, toolCalls, start), nil
		}

		if len(reply.ToolCalls) == 0 {
			// 模型文本回答:不接受,推它一次;最后一轮仍不表态就判定失败
			if last {
				negotiationResult.Reason = "model did not call " + IntentionToolStart
				return n.finish(negotiationResult, round, toolCalls, start), nil
			}

			messages = append(messages, buildSimpleAssistantMessage(reply.Content), buildUserMessage(fmt.Sprintf("不要用文本回答。请用%s明确表态。", IntentionToolStart)))
			continue
		}

		messages = append(messages, buildAssistantMessage(reply.Content, reply.ToolCalls))

		for _, call := range reply.ToolCalls {
			toolCalls++
			if call.Function.Name == IntentionToolStart {
				parsed, err := parseNegotiationStartArg(call.Function.Arguments)
				if err != nil {
					//参数坏了:当成一次失败的探测,回灌错误让模型重试
					logging.LogWarnF("plan negotiate:invalid %s args,skill=%s,round=%d,error=%s,raw=%s",
						IntentionToolStart, req.SkillId, round, err.Error(), pCommon.TruncateRunes(call.Function.Arguments, 512, cCommon.TruncatedMarker))
					messages = append(messages, buildToolMessage(call.ID, err.Error()))
					continue
				}

				parsed.Rounds = round
				parsed.ToolCalls = toolCalls

				return n.finish(parsed, round, toolCalls, start), nil
			}

			body := n.execProbe(req, call)
			messages = append(messages, buildToolMessage(call.ID, pCommon.TruncateRunes(body, 4000, cCommon.TruncatedMarker)))
		}
	}

	negotiationResult.Reason = "negotiate rounds exhausted"
	return n.finish(negotiationResult, maxRounds, toolCalls, start), nil
}

// ============================= private functions & methods =====================
func (n *IntentionNegotiator) buildIntentionSystemPrompt(req NegotiationRequest) string {
	var builder strings.Builder

	builder.WriteString("你是技能调度判定器。你只需要做两件事：\n")
	builder.WriteString("1）判断用户这句话是否需要执行本技能的SOP流程；")
	builder.WriteString("2）若需要执行，给出执行所需的参数。\n\n")

	builder.WriteString("【技能】" + req.SkillId + "\n")
	if req.SkillName != pCommon.BlankString {
		builder.WriteString("【名称】" + req.SkillName + "\n")
	}
	if req.SkillDesc != pCommon.BlankString {
		builder.WriteString("【简介】" + req.SkillDesc + "\n")
	}

	if len(req.SOPSteps) > 0 {
		builder.WriteString("\n【SOP 流程】\n")
		for i, step := range req.SOPSteps {
			builder.WriteString(fmt.Sprintf("%d. %s\n", i+1, step))
		}
	}

	builder.WriteString("\n【可用工具】只有 " + IntentionToolFsList + " / " + IntentionToolFsStat +
		":只读,返回文件名、大小与扩展名统计,不返回文件内容。\n")

	// 只保留机制性规则:可被校验器/schema 强化;业务判据走 IntentGuard
	builder.WriteString("\n【机制规则】\n")
	builder.WriteString("1. 必须调用 " + IntentionToolStart + " 表态,不要用普通文本回答。\n")
	builder.WriteString("2. 参数必须真实存在:不确定就用 " + IntentionToolFsList + "/" +
		IntentionToolFsStat + " 验证;**不要编造路径**。\n")
	builder.WriteString("3. 信息不足时如实说明缺什么,不要用默认值填充。\n")
	builder.WriteString("4. 本技能的完整说明见其 SKILL.md;下面的【技能判定说明】优先于你的直觉。\n")

	if req.IntentionGuard != pCommon.BlankString {
		builder.WriteString("\n 【技能判定说明(来自SKILL.md)】" + req.IntentionGuard + "\n")
	}

	if req.Feedback != pCommon.BlankString {
		builder.WriteString("\n【上一次提交被校验器拒绝】\n" + req.Feedback + "\n")
		builder.WriteString("请修正后用 " + IntentionToolStart + " 重新提交。\n")
	}

	return builder.String()
}

func (n *IntentionNegotiator) buildUserPrompt(req NegotiationRequest) string {
	return "用户原话：" + strings.TrimSpace(req.Question)
}

func (n *IntentionNegotiator) finish(result NegotiationResult, rounds int, toolCalls int, start time.Time) NegotiationResult {
	result.Rounds = rounds
	result.ToolCalls = toolCalls
	result.ElapsedMs = pCommon.MilliDiff(start)
	if result.Reason == pCommon.BlankString {
		result.Reason = "no reason provided"
	}

	return result
}

func (n *IntentionNegotiator) roundTools(skillId string, last bool) ([]llmproto.Tool, string) {
	if last {
		// 有些LLM Provider 不支持 ToolChoiceRequire模式
		return []llmproto.Tool{intentionStart(skillId)}, llmproto.ToolChoiceAuto
	}

	tools := make([]llmproto.Tool, 0, 3)
	tools = append(tools, probeTool()...)
	tools = append(tools, intentionStart(skillId))

	return tools, llmproto.ToolChoiceAuto
}

func (n *IntentionNegotiator) execProbe(req NegotiationRequest, call llmproto.ToolCall) string {
	switch call.Function.Name {
	case IntentionToolFsList:
		body, err := executeFsList(call.Function.Arguments, req.WorkRoot, req.Rules)
		if err != nil {
			return `{"error":` + pCommon.ReplaceStringWithReplacer(err.Error(), `\`, `\\`, `"`, `\"`, "\n", `\n`) + `}`
		}

		return body
	case IntentionToolFsStat:
		body, err := executeFsStat(call.Function.Arguments, req.WorkRoot, req.Rules)
		if err != nil {
			return `{"error":` + pCommon.ReplaceStringWithReplacer(err.Error(), `\`, `\\`, `"`, `\"`, "\n", `\n`) + `}`
		}
		return body
	default:
		logging.LogWarnF("plan negotiator:unknown tool[%s] requested", call.Function.Name)
		return `{"error":"unknown tool, only fs_list/fs_stat/` + IntentionToolStart + ` are available"}`
	}
}
