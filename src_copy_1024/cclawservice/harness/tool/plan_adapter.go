package tool

import (
	"my.favorite/cclawservice/chat"
	"my.favorite/cclawservice/harness/api/llmproto"
)

type RoundPlannerAdapter struct {
	plan  FirstRoundToolPlan
	scope string
}

func (rpAdapter RoundPlannerAdapter) RoundTools(all []llmproto.Tool, round int) (tools []llmproto.Tool, toolChoice string) {
	return rpAdapter.plan.RoundTools(all, round)
}

func (rpAdapter RoundPlannerAdapter) Verify(calls []llmproto.ToolCall, toolChoice string) {
	VerifyForcedCall(rpAdapter.plan, calls, toolChoice, rpAdapter.scope)
}

var _ chat.IRoundPlanner = RoundPlannerAdapter{}
