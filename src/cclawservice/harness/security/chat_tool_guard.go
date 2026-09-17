/**
* 工具调用死循环守卫的"一轮对话"编排
*
* 职责:把本包 LoopGuard(纯判定)接进工具循环,并固化两条编排语义:
*   1. 命中 → 把守卫提示追加到该次工具结果之后回填给模型(提示语是写给模型的,不进 messages 就不起作用);
*   2. 提示过一次后模型**再次命中**守卫 → 判定为无法收敛,由调用方终止本轮(避免空转到 AgentToolLoopMax)。
*      只判"是否还调用工具"会误杀"听到提示后换了方法"的正常轮次(提示语本身就是"请换一种方法/方式")。
*
* 生命周期(关键):一轮对话一个实例
**/

package security

import (
	"strings"

	cCommon "my.favorite/cclawservice/common"
	myException "my.favorite/cclawservice/exception"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
)

// maxGuardHitsBeforeStop 避免重复提示
const maxGuardHitsBeforeStop = 1

// TurnLoopGuard 一轮对话的工具死循环守卫
//
// 生命周期(关键):一轮对话一个实例
type TurnLoopGuard struct {
	guard *LoopGuard
	hits  int              // 守卫命中次数(每次命中都会向模型追加一次提示)
	hint  string           // 首次命中的提示(保留首次:终止时的错误细节应是"最早那条",不被后续命中覆盖)
	last  LoopGuardVerdict // 最近一次命中的结构化判定(日志/用户提示分级用)
}

// NewTurnLoopGuard 创建一轮对话的守卫:每轮必须新建。
func NewTurnLoopGuard() *TurnLoopGuard {
	return &TurnLoopGuard{guard: NewLoopGuard()}
}

// Check 判定一次**已执行完**的工具调用,返回需要回填给模型的守卫提示(未命中返回空串)。
func (g *TurnLoopGuard) Check(toolName, toolArgs string, resultOK bool, result string) string {
	verdict := g.guard.Check(toolName, toolArgs, resultOK, result)
	if !verdict.Hit() {
		return pCommon.BlankString
	}

	g.hits += 1
	g.last = verdict
	if g.hint == pCommon.BlankString {
		g.hint = verdict.Msg //保留首次提示
	}

	return verdict.Msg
}

// LastVerdict 最近一次命中的结构化判定(未命中时为零值):供日志/用户提示分级使用。
func (g *TurnLoopGuard) LastVerdict() LoopGuardVerdict { return g.last }

// Hits 返回本轮守卫命中次数(可观测性/测试用)。
func (g *TurnLoopGuard) Hits() int { return g.hits }

// HintedOnce 本轮是否已向模型回填过守卫提示。
func (g *TurnLoopGuard) HintedOnce() bool { return g.hits > 0 }

// ShouldStop 是否应终止本轮
func (g *TurnLoopGuard) ShouldStop() bool { return g.hits > maxGuardHitsBeforeStop }

// StopError 再次命中守卫时的终止错误
func (g *TurnLoopGuard) StopError(scope string) error {
	return myException.ChatToolLoopGuardKind.Newf("%s: stopped by loop guard: %s", scope, g.hint)
}

// ApplyLoopGuard 判定一次已经执行一次的工具调用，并完成命中后的三件事
// 1.写日志、2.给用户一条提示(流式路径)、3.把守卫提示回填给模型，返回回填的内容
func (g *TurnLoopGuard) ApplyLoopGuard(toolName, toolArgs string, resultOK bool, result string,
	onNotice func(code, msg string)) string {
	hint := g.Check(toolName, toolArgs, resultOK, result)
	if hint == pCommon.BlankString {
		return result
	}

	verdict := g.LastVerdict()
	logging.LogWarnF("chat service: tool loop guard hit: rule=%s level=%s hits=%d tool=%s hint=%s",
		verdict.Rule, verdict.Level, g.Hits(), toolName, hint)

	//首次命中：以onNotice 让用户知道“模型在重复调用工具,已提醒它换策略”
	//再次命中：以chat.tool_loop_guard错误收尾,不会重复下发
	if onNotice != nil && !g.ShouldStop() {
		onNotice(cCommon.ChatNoticeToolLoopGuard, GetLoopGuardLevelMsg(verdict.Level))
	}

	return ToolResultWithHint(result, hint)
}

// GetLoopGuardLevelMsg 按照判定级别选用户的话术：提醒/停止重试
func GetLoopGuardLevelMsg(level LoopGuardLevel) string {
	if level == LoopLevelStop {
		return cCommon.ConstChatNoticeToolLoopGuardStopMsg
	}

	return cCommon.ConstChatNoticeToolLoopGuardWarnMsg
}

// ToolResultWithHint 组装回填给模型的工具结果:命中的提示追加在原始结果之后,
// 原始结果必须保留(模型仍需要看到真实输出/报错);tool_call_id 配对由调用方保持不变。
func ToolResultWithHint(result, hint string) string {
	if strings.TrimSpace(hint) == pCommon.BlankString {
		return result
	}

	return result + "\n\n" + hint
}
