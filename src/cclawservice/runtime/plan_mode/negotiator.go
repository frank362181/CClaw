/**
* 意图协商器:一个独立的针对用户问题的协商工具
*
* 设计意图:为何不塞进ToolLoop/TurnRunner
* - 协商阶段只允许只读工具 + 一个终止工具,和对话循环的工具集不同
* - 协商不写历史、不占并发、不需要装配器和记账
* - 独立实现,ToolLoop/TurnRunner 契约 0 改动
**/

package plan_mode

import (
	"context"
	"fmt"
	"time"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/common/llmproto"
	"my.favorite/cclawservice/config"
	chatCommon "my.favorite/cclawservice/runtime/common"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"

	"my.favorite/cclawservice/runtime/common"
)

type IntentionNegotiator struct {
	transport common.ITransportClient
	config    config.ChatConfig
}

var _ INegotiator = (*IntentionNegotiator)(nil)

func NewIntentionNegotiator(transport common.ITransportClient, config config.ChatConfig) INegotiator {
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

	messages := chatCommon.BuildSimpleChatMessage(BuildIntentionSystemPrompt(req), BuildUserPrompt(req))
	negotiationResult := NegotiationResult{Decision: DecisionFailed}
	toolCalls := 0
	for round := 1; round <= maxRounds; round++ {
		if err := runCtx.Err(); err != nil {
			negotiationResult.Reason = "negotiate canceled:" + err.Error()
			return n.onFinished(negotiationResult, round, toolCalls, start), nil
		}

		last := round == maxRounds
		tools, choice := GetNegotiationTools(req.SkillId, req.NegSpec, last)
		reply, err := n.transport.Complete(runCtx, common.Provider{Llm: req.Provider}, llmproto.LlmRequest{
			Messages:        messages,
			Tools:           tools,
			ToolChoice:      choice,
			Streamed:        false,
			MaxTokens:       1024,
			DisableThinking: true, // 判定任务不需要思考链:省token、降延迟、必考与tool_choice的互斥
		})
		if err != nil {
			negotiationResult.Reason = "negotiate upcall failed:" + err.Error()
			return n.onFinished(negotiationResult, round, toolCalls, start), nil
		}

		if len(reply.ToolCalls) == 0 {
			// 模型文本回答:不接受,推它一次;最后一轮仍不表态就判定失败
			if last {
				negotiationResult.Reason = "model did not call " + IntentionToolStart
				return n.onFinished(negotiationResult, round, toolCalls, start), nil
			}

			messages = append(messages, chatCommon.BuildSimpleAssistantMessage(reply.Content), chatCommon.BuildUserMessage(fmt.Sprintf("不要用文本回答。请用%s明确表态。", IntentionToolStart)))
			continue
		}

		messages = append(messages, chatCommon.BuildAssistantMessage(reply.Content, reply.ToolCalls))

		for _, call := range reply.ToolCalls {
			toolCalls++
			if call.Function.Name == IntentionToolStart {
				parsed, err := ParseNegotiationStartArg(call.Function.Arguments, req.NegSpec)
				if err != nil {
					//参数坏了:当成一次失败的探测,回灌错误让模型重试
					logging.LogWarnF("plan negotiate:invalid %s args,skill=%s,round=%d,error=%s,raw=%s",
						IntentionToolStart, req.SkillId, round, err.Error(), pCommon.TruncateRunes(call.Function.Arguments, 512, cCommon.TruncatedMarker))
					messages = append(messages, chatCommon.BuildToolMessage(call.ID, err.Error()))
					continue
				}

				parsed.Rounds = round
				parsed.ToolCalls = toolCalls

				return n.onFinished(parsed, round, toolCalls, start), nil
			}

			body := ExecProbe(req, call)
			messages = append(messages, chatCommon.BuildToolMessage(call.ID, pCommon.TruncateRunes(body, 4000, cCommon.TruncatedMarker)))
		}
	}

	negotiationResult.Reason = "negotiate rounds exhausted"
	return n.onFinished(negotiationResult, maxRounds, toolCalls, start), nil
}

// ============================= private functions & methods =====================

func (n *IntentionNegotiator) onFinished(result NegotiationResult, rounds int, toolCalls int, start time.Time) NegotiationResult {
	result.Rounds = rounds
	result.ToolCalls = toolCalls
	result.ElapsedMs = pCommon.MilliDiff(start)
	if result.Reason == pCommon.BlankString {
		result.Reason = "no reason provided"
	}

	return result
}
