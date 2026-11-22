/**
* 对用户输入问题进行意图识别,根据识别结果进行简单一轮问题回复,还会通过Plan Mode进行问答
*
* 职责边界(参见 document/multi-agent协作实现方案.md)
*	- 模型:决定 decision + 填 spec
*	- 校验器(biz):否决不可用的spec
*	- 本文见只是描述"协商的结果长什么样",不含任何决策
**/

package plan_mode

import (
	"context"
	"time"

	chatCommon "my.favorite/cclawservice/chat/common"
	"my.favorite/cclawservice/harness/toolexecutor"
	"my.favorite/cclawservice/model/dataobject"
)

type NegotiationDecision string // 协商结论
const (
	DecisionProceed        NegotiationDecision = "proceed"            // 执行本技能的SOP
	DecisionNeedMoreInfo   NegotiationDecision = "need_more_info"     // 信息不足,需要用户补充
	DecisionNotApplicable  NegotiationDecision = "not_applicable"     // 仅询问本技能/工具的用法,不需要走plan mode
	DecisionValidateFailed NegotiationDecision = "validate_failed"    // 校验多次失败
	DecisionFailed         NegotiationDecision = "negotiation_failed" // 协商本轮失败(超时/未表态/上游错误)
)

func (d NegotiationDecision) Valid() bool {
	switch d {
	case DecisionProceed, DecisionNeedMoreInfo, DecisionNotApplicable,
		DecisionValidateFailed, DecisionFailed:
		return true
	default:
		return false
	}
}

// NegotiationRequest 协商请求
type NegotiationRequest struct {
	Provider  dataobject.TLlm // LLM Provider
	Question  string
	SkillId   string
	SkillName string
	SkillDesc string

	SOPSteps []string // 计划骨架摘要(每行一个步骤),只用于让模型理解"这套流程要做什么"

	WorkRoot string
	Rules    toolexecutor.ToolConfigRules

	// 业务侧实现:参数 schema / 解析 / 判定说明
	NegSpec chatCommon.INegotiationSpec

	Feedback  string // 上一次校验失败的说明(重试时回灌给模型)
	MaxRounds int
	Timeout   time.Duration
}

// NegotiationResult 一次协商的结果
type NegotiationResult struct {
	Decision NegotiationDecision
	Reason   string
	Missing  []string

	// 业务参数,有INegotiationSpec.DecodeParams产出
	Arg any

	Rounds    int
	ToolCalls int
	ElapsedMs int64
}

// Planned 是否应当进入plan mode模式
func (n NegotiationResult) Planned() bool {
	return n.Decision == DecisionProceed
}

// INegotiator 协商接口
type INegotiator interface {
	Negotiate(ctx context.Context, request NegotiationRequest) (NegotiationResult, error)
}

type NopNegotiator struct {
}

func (NopNegotiator) Negotiate(ctx context.Context, request NegotiationRequest) (NegotiationResult, error) {
	return NegotiationResult{
		Decision: DecisionNotApplicable,
		Reason:   "negotiator disabled",
	}, nil
}
