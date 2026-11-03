/*
* 串行/并行 共用的单次执行实现
**/

package toolexecutor

import (
	"context"
	"fmt"

	pCommon "my.favorite/platformutility/common"
)

// executorCore 串行/并行 共用的单次执行实现
type executorCore struct {
	runner IScriptRunner
}

// executeOne 执行一次工具调用,返回自洽的 ToolOutcome
func (e *executorCore) executeOne(ctx context.Context, env ToolEnv, invocation ToolInvocation,
	ledger ToolLedger, onLine func(string)) (oc ToolOutcome) {

	oc = ToolOutcome{
		Seq:    invocation.Seq,
		CallID: invocation.ID(),
		Name:   invocation.Name(),
		Arg:    invocation.Args(),
		CallAt: pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp,
	}

	// 先登记"已发起",再做任何可能失败的事
	// 中断时后面的代码可能根本走不到,登记太晚也会让这次调用彻底消失
	ledgerIdx := -1

	defer func() {
		if r := recover(); r != nil {
			oc.Full = pCommon.BlankString
			oc.Reason = fmt.Errorf("tool %s panicked: %v", oc.Name, r)
		}

		if oc.RespAt == 0 {
			oc.RespAt = pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
		}

		if ledgerIdx >= 0 {
			ledger.Finish(ledgerIdx, oc.RespAt, oc.ForRecord())
		}
	}()

	ledgerIdx = ledger.Start(oc.Name, oc.CallAt)

	// 规则检查
	permission := env.AdmitRule(invocation)
	if !permission.Allow {
		oc.Reason = permission.Error(oc.Name)
		return oc // 不执行,保持原样,便于复盘
	}

	toolSpec, err := env.Spec(invocation.Name())
	if err != nil {
		oc.Reason = err // 不进执行器,空路径只会执行失败
		return oc
	}

	start := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().MilliTimestamp
	oc.Full, oc.Reason = e.runner.Run(ctx, ScriptArgument{
		Path:     toolSpec.Script,
		Args:     permission.Arguments(oc.Arg), // 为将来Hook改写参数提供扩展
		WorkRoot: env.WorkRoot,
		OnLine:   onLine, // nil => 无进度消费
		Sandbox:  BuildSandBoxSpec(env, toolSpec),
	})
	oc.ElapseMs = pCommon.MilliDiff2(start)
	return oc
}
