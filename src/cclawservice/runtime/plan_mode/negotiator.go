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
	"strings"
	"time"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/common/llmproto"
	"my.favorite/cclawservice/config"
	chatCommon "my.favorite/cclawservice/runtime/common"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"

	"my.favorite/cclawservice/runtime/common"
)

const (
	// 单轮协商的最小时间
	minRoundTimeout = 8 * time.Second

	// 单轮次探测的最小时间片
	minProbeTimeout = 10 * time.Second

	intentionMaxTokens = 2048
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

	maxRounds := req.MaxRounds
	if maxRounds <= 0 {
		maxRounds = n.config.Negotiation.Rounds
	}
	if maxRounds < 1 {
		maxRounds = 1
	}

	// 总预算 + 每轮预算:避免一次LLM慢响应就将整个协商过程拖死
	totalCtx, cancelTotal := context.WithTimeout(ctx, timeout)
	defer cancelTotal()

	preRound := timeout / time.Duration(maxRounds)
	if preRound < minRoundTimeout {
		preRound = minRoundTimeout
	}

	// 给首轮留60%的时间
	if first := timeout * 6 / 10; first > preRound {
		preRound = first
	}

	messages := chatCommon.BuildSimpleChatMessage(BuildIntentionSystemPrompt(req), BuildUserPrompt(req))
	negotiationResult := NegotiationResult{Decision: DecisionFailed}
	toolCalls := 0
	terminalRejected := 0

	for round := 1; round <= maxRounds; round++ {
		if err := totalCtx.Err(); err != nil {
			negotiationResult.Reason = "negotiation canceled:" + err.Error()
			return n.onFinished(negotiationResult, round-1, toolCalls, start), nil
		}

		last := round == maxRounds
		tools, choice := GetNegotiationTools(req.SkillId, req.NegSpec, req.SystemTools, last)
		if last {
			if req.Provider.ThinkingLocked {
				choice = llmproto.ToolChoiceAuto
			} else {
				choice = llmproto.ToolChoiceRequired
			}
		}

		reply, err := n.negotiateOnce(totalCtx, req, messages, tools, choice, preRound)

		if err != nil && last && strings.Contains(strings.ToLower(err.Error()), "tool_choice") {
			// 兜底,万一llm.thinking_locked模式配置错误,LLM端拒收 required，返回auto再来一次
			logging.LogWarnF("plan negotiate: provider rejected tool_choice=%s, fallback auto, turn=%s, round=%d", choice, req.TurnID, round)
			choice = llmproto.ToolChoiceAuto
			reply, err = n.negotiateOnce(totalCtx, req, messages, tools, choice, preRound)
		}

		if err != nil {
			if totalCtx.Err() == nil && round < maxRounds {
				// 本轮超时,但总的尚未超时,留给下一轮
				logging.LogWarnF("plan negotiation: round %d failed,keep remaining budget,turn=%s,err=%v", round, req.TurnID, err)
				continue
			}

			negotiationResult.Reason = "negotiation upcall failed:" + err.Error()
			return n.onFinished(negotiationResult, round-1, toolCalls, start), nil
		}

		// 观测性
		logging.LogInfoF("plan negotiate: turn=%s round=%d/%d last=%v choice=%s locked=%v finish=%s calls=%d names=%s",
			req.TurnID, round, maxRounds, last, choice, req.Provider.ThinkingLocked, reply.FinishReason,
			len(reply.ToolCalls),
			pCommon.TruncateRunes(DescribeToolCalls(reply.ToolCalls), 400, cCommon.TruncatedMarker))

		if len(reply.ToolCalls) == 0 {
			// 模型文本回答:不接受,再推一次
			if last {
				if raw, ok := pCommon.ExtractJsonObjectFromString(reply.Content); ok {
					// 兜底:固有思考模型拿不到required时,常见做法是将结论写成"我选择如下..."
					if parsed, pErr := ParseNegotiationStartArg(raw, req.NegSpec); pErr == nil {
						logging.LogInfoF("plan negotiation: terminal decision parsed from text, turn=%s,round=%d", req.TurnID, round)
						parsed.Rounds = round
						parsed.ToolCalls = toolCalls
						return n.onFinished(parsed, round, toolCalls, start), nil
					}
				}

				negotiationResult.Reason = "model did not call " + IntentionToolStart
				return n.onFinished(negotiationResult, round, toolCalls, start), nil
			}

			messages = append(messages,
				chatCommon.BuildSimpleAssistantMessage(reply.Content),
				chatCommon.BuildUserMessage(fmt.Sprintf("不要用文本回答。请用%s明确表态。", IntentionToolStart)))
			continue
		}

		messages = append(messages, chatCommon.BuildAssistantMessage(reply.Content, reply.ToolCalls))
		for _, call := range reply.ToolCalls {
			toolCalls++

			if IsIntentionStart(call.Function.Name) {
				parsed, err := ParseNegotiationStartArg(call.Function.Arguments, req.NegSpec)
				if err != nil {
					// 参数坏了：当成一次失败的探测,回填错误信息让模型重试
					logging.LogWarnF("plan negotiate:invalid %s args,turn=%s,round=%d,error=%s,raw=%s",
						IntentionToolStart, req.TurnID, round, err.Error(),
						pCommon.TruncateRunes(call.Function.Arguments, 512, cCommon.TruncatedMarker))
					messages = append(messages, chatCommon.BuildToolMessage(call.ID, err.Error()))
					continue
				}
				parsed.Rounds = round
				parsed.ToolCalls = toolCalls

				return n.onFinished(parsed, round, toolCalls, start), nil
			}

			// 终止轮防护:本轮工具列表里只有 intention_start
			// 模型仍可能幻想出探测工具
			terminalRejected++
			if last {
				logging.LogWarnF("plan negotiate: probe rejected in terminal round,turn=%s,round=%d,tool=%s", req.TurnID, round, call.Function.Name)
				messages = append(messages, chatCommon.BuildToolMessage(call.ID, `{"error":"terminal round: probe tools are disabled, you must call intention_start"}`))

				if terminalRejected == 1 {
					messages = append(messages, chatCommon.BuildUserMessage("本轮探测工具已禁用,已探测到的结果都在上面。请立刻调用 "+IntentionToolStart+
						" 表态(decision/spec/targets),不要再调用任何其他工具。"))
				}
				continue
			}

			// 探测必须有独立的上下文
			probeCtx, probeCancel := context.WithTimeout(totalCtx, minProbeTimeout)
			body := ExecProbe(probeCtx, req, call)
			probeCancel()

			messages = append(messages, chatCommon.BuildToolMessage(call.ID, pCommon.TruncateRunes(body, 4000, cCommon.TruncatedMarker)))
		}
	}

	negotiationResult.Reason = NegotiationResultRoundsExhausted
	return n.onFinished(negotiationResult, maxRounds, toolCalls, start), nil
}

// ============================= private functions & methods =====================

func (n *IntentionNegotiator) negotiateOnce(parent context.Context, req NegotiationRequest, messages []llmproto.ChatMessage,
	tools []llmproto.Tool, choice string, roundTimeout time.Duration) (llmproto.LlmReply, error) {
	roundCtx, cancelRound := context.WithTimeout(parent, roundTimeout)
	defer cancelRound()

	return n.transport.Complete(roundCtx, common.Provider{Llm: req.Provider}, llmproto.LlmRequest{
		Messages:        messages,
		Tools:           tools,
		ToolChoice:      choice,
		Streamed:        false,
		MaxTokens:       intentionMaxTokens,
		DisableThinking: true, // 判定任务不需要思考模式,save token
	})
}

func (n *IntentionNegotiator) onFinished(result NegotiationResult, rounds int, toolCalls int, start time.Time) NegotiationResult {
	result.Rounds = rounds
	result.ToolCalls = toolCalls
	result.ElapsedMs = pCommon.MilliDiff(start)
	if result.Reason == pCommon.BlankString {
		result.Reason = NegotiationResultNoReasonProvide
	}

	return result
}

/***
start := pCommon.GetDatetimeUtilityInstance().GetNow()
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = time.Duration(n.config.Negotiation.ArgTimeout) * time.Second
	}

	maxRounds := req.MaxRounds
	if maxRounds <= 0 {
		maxRounds = n.config.Negotiation.Rounds
	}
	if maxRounds < 1 {
		maxRounds = 1
	}

	// 总预算 + 每轮预算:原来所有轮次共用 20 seconds
	// 可能一次LLM慢响应会将整个协商过程拖死,分轮限时
	totalCtx, cancelTotal := context.WithTimeout(ctx, timeout)
	defer cancelTotal()

	preRound := timeout / time.Duration(maxRounds)
	if preRound < minRoundTimeout {
		preRound = minRoundTimeout
	}

	messages := chatCommon.BuildSimpleChatMessage(BuildIntentionSystemPrompt(req), BuildUserPrompt(req))
	negotiationResult := NegotiationResult{Decision: DecisionFailed}
	toolCalls := 0
	for round := 1; round <= maxRounds; round++ {
		if err := totalCtx.Err(); err != nil {
			negotiationResult.Reason = "negotiate canceled:" + err.Error()
			return n.onFinished(negotiationResult, round-1, toolCalls, start), nil
		}

		roundCtx, cancelRound := context.WithTimeout(totalCtx, preRound)
		last := round == maxRounds
		tools, choice := GetNegotiationTools(req.SkillId, req.NegSpec, req.SystemTools, last)
		reply, err := n.transport.Complete(roundCtx, common.Provider{Llm: req.Provider}, llmproto.LlmRequest{
			Messages:        messages,
			Tools:           tools,
			ToolChoice:      choice,
			Streamed:        false,
			MaxTokens:       1024,
			DisableThinking: true, // 判定任务不需要思考链:省token、降延迟、必考与tool_choice的互斥
		})
		cancelRound() // 协商结束,取消协商的超时

		if err != nil {
			if totalCtx.Err() == nil && round < maxRounds {
				// 本轮超时,但总时间还没超时,留给下一轮
				logging.LogWarnF("plan negotiation: round %d timeout,retry with terminal tool only,turn:%s", round, req.Question)
				continue
			}
			negotiationResult.Reason = "negotiate upcall failed:" + err.Error()
			return n.onFinished(negotiationResult, round, toolCalls, start), nil
		}

		if len(reply.ToolCalls) == 0 {
			// 模型文本回答:不接受,推它一次;最后一轮仍不表态就判定失败
			if last {
				negotiationResult.Reason = "model did not call " + IntentionToolStart
				return n.onFinished(negotiationResult, round, toolCalls, start), nil
			}

			messages = append(messages,
				chatCommon.BuildSimpleAssistantMessage(reply.Content),
				chatCommon.BuildUserMessage(fmt.Sprintf("不要用文本回答。请用%s明确表态。", IntentionToolStart)))
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

			// 调用超时
			probeCtx, cancelProbe := context.WithTimeout(totalCtx, minProbeTimeout)
			body := ExecProbe(probeCtx, req, call)
			cancelProbe() // 工具调用超时结束

			messages = append(messages, chatCommon.BuildToolMessage(call.ID, pCommon.TruncateRunes(body, 4000, cCommon.TruncatedMarker)))
		}
	}

	negotiationResult.Reason = "negotiate rounds exhausted"
	return n.onFinished(negotiationResult, maxRounds, toolCalls, start), nil
***/
