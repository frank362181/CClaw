/**
* 工具调用死循环守卫的"一轮对话"编排测试(纯判定/编排层)
*
* 覆盖 TurnLoopGuard 的"一轮"语义:每轮新建互不共享、按调用顺序判定、失败同样计数、
* "再次命中才终止"(ShouldStop)、保留首次提示、回填内容保留原始结果、终止错误带聊天域 Kind。
*
* 注意:本文件只依赖 ctxmgr 自身与 exception/apperr,不得 import harness / service
* (它们都 import ctxmgr,会构成测试期 import cycle)。工具循环的端到端测试在 service 包。
**/

package ctxmgr

import (
	"fmt"
	"strings"
	"testing"

	"my.favorite/cclawservice/apperr"
	myException "my.favorite/cclawservice/exception"
	pCommon "my.favorite/platformutility/common"
)

// loopGuardHintMarker 守卫提示里稳定出现的片段(用于断言"提示内容正确")。
const loopGuardHintMarker = "检测到死循环"

// ---------------------------------------------------------------------------
// 一轮对话的语义
// ---------------------------------------------------------------------------

// TestTurnLoopGuard_NotSharedBetweenTurns 验证"每轮新建"这条契约:
// 上一轮命中(甚至已提示)不影响下一轮的新实例,计数必须从 0 重新累计。
func TestTurnLoopGuard_NotSharedBetweenTurns(t *testing.T) {
	prevTurn := NewTurnLoopGuard()
	for i := 1; i <= 3; i++ {
		prevTurn.Check("read_file", `{"path":"a.txt"}`, true, "same content")
	}
	if !prevTurn.HintedOnce() {
		t.Fatal("同一工具连续 3 次完全相同调用应命中守卫")
	}

	nextTurn := NewTurnLoopGuard()
	for i := 1; i <= 2; i++ {
		if hint := nextTurn.Check("read_file", `{"path":"a.txt"}`, true, "same content"); hint != pCommon.BlankString {
			t.Fatalf("新一轮第 %d 次调用不应命中守卫(计数必须重新开始), got %q", i, hint)
		}
	}
	if hint := nextTurn.Check("read_file", `{"path":"a.txt"}`, true, "same content"); strings.TrimSpace(hint) == pCommon.BlankString {
		t.Fatal("新一轮连续 3 次完全相同调用应命中守卫")
	}
	if nextTurn.Hits() != 1 {
		t.Errorf("新一轮命中次数 = %d, want 1", nextTurn.Hits())
	}
}

// TestTurnLoopGuard_ShouldStopOnlyAfterSecondHit 验证终止条件是"提示后**再次命中**"而不是"提示后继续调用工具":
// 第一次命中只提示(给模型换策略的机会),第二次命中才判定为无法收敛。
func TestTurnLoopGuard_ShouldStopOnlyAfterSecondHit(t *testing.T) {
	g := NewTurnLoopGuard()

	for i := 1; i <= 2; i++ {
		if hint := g.Check("read_file", `{"path":"a.txt"}`, true, "same"); hint != pCommon.BlankString {
			t.Fatalf("第 %d 次调用不应命中守卫, got %q", i, hint)
		}
	}
	if g.Hits() != 0 || g.ShouldStop() {
		t.Fatalf("未命中时不应终止: hits=%d shouldStop=%v", g.Hits(), g.ShouldStop())
	}

	// 第 3 次:首次命中 → 只提示
	if hint := g.Check("read_file", `{"path":"a.txt"}`, true, "same"); strings.TrimSpace(hint) == pCommon.BlankString {
		t.Fatal("连续 3 次完全相同调用应命中守卫")
	}
	if g.Hits() != 1 || g.ShouldStop() {
		t.Fatalf("首次命中只应提示、不应终止: hits=%d shouldStop=%v", g.Hits(), g.ShouldStop())
	}

	// 第 4 次:仍然完全相同 → 再次命中 → 判定为无法收敛
	g.Check("read_file", `{"path":"a.txt"}`, true, "same")
	if g.Hits() != 2 || !g.ShouldStop() {
		t.Fatalf("再次命中应判定为终止: hits=%d shouldStop=%v", g.Hits(), g.ShouldStop())
	}
}

// TestTurnLoopGuard_KeepsFirstHint 验证保留**首次**命中提示(不被后续命中覆盖,便于排障定位最早那条)。
func TestTurnLoopGuard_KeepsFirstHint(t *testing.T) {
	g := NewTurnLoopGuard()

	// 先触发"连续失败"(规则 2)提示
	for i := 1; i <= maxConsecutiveCallErrors; i++ {
		g.Check(fmt.Sprintf("tool_%d", i), fmt.Sprintf(`{"n":%d}`, i), false, fmt.Sprintf("err-%d", i))
	}
	// 再触发"完全相同调用"(规则 4)命中:不应覆盖首次提示
	for i := 1; i <= maxIdenticalToolCallings; i++ {
		g.Check("read_file", `{"path":"a.txt"}`, true, "same")
	}
	if g.Hits() < 2 {
		t.Fatalf("命中次数 = %d, want >= 2", g.Hits())
	}

	err := g.StopError("test")
	want := fmt.Sprintf(maxConsecutiveCallErrorsStr, maxConsecutiveCallErrors)
	if !strings.Contains(err.Error(), want) {
		t.Errorf("错误细节应保留首次命中提示 %q, got %v", want, err)
	}
}

// TestTurnLoopGuard_CountsInCallOrder 验证按"模型声明的调用顺序"判定:二元交替(A,B,A,B,A,B)在第 6 次命中。
func TestTurnLoopGuard_CountsInCallOrder(t *testing.T) {
	g := NewTurnLoopGuard()
	seq := []struct {
		name   string
		args   string
		result string
	}{
		{"read_file", `{"path":"a.txt"}`, "content-a"},
		{"run_cmd", `{"cmd":"ls"}`, "cmd-out"},
		{"read_file", `{"path":"a.txt"}`, "content-a"},
		{"run_cmd", `{"cmd":"ls"}`, "cmd-out"},
		{"read_file", `{"path":"a.txt"}`, "content-a"},
		{"run_cmd", `{"cmd":"ls"}`, "cmd-out"},
	}

	for i, s := range seq {
		hint := g.Check(s.name, s.args, true, s.result)
		if i < len(seq)-1 {
			if hint != pCommon.BlankString {
				t.Fatalf("第 %d 次调用不应命中守卫: %q", i+1, hint)
			}
			continue
		}
		if strings.TrimSpace(hint) == pCommon.BlankString {
			t.Fatalf("A/B 交替第 %d 次调用应命中守卫", i+1)
		}
		if !strings.Contains(hint, "read_file") || !strings.Contains(hint, "run_cmd") {
			t.Errorf("提示信息应点出交替的两个工具, got %q", hint)
		}
	}
}

// TestTurnLoopGuard_CountsFailedCalls 验证失败调用同样参与判定(连续失败规则)。
func TestTurnLoopGuard_CountsFailedCalls(t *testing.T) {
	g := NewTurnLoopGuard()

	for i := 1; i < maxConsecutiveCallErrors; i++ {
		// 工具名与结果每次都不同 → 只可能命中"连续失败"规则
		if hint := g.Check(fmt.Sprintf("tool_%d", i), fmt.Sprintf(`{"n":%d}`, i), false, fmt.Sprintf("error-%d", i)); hint != pCommon.BlankString {
			t.Fatalf("第 %d 次失败调用不应命中守卫: %q", i, hint)
		}
	}
	if hint := g.Check("tool_x", `{"n":99}`, false, "error-99"); strings.TrimSpace(hint) == pCommon.BlankString {
		t.Fatalf("连续第 %d 次失败调用应命中守卫", maxConsecutiveCallErrors)
	}
}

// ---------------------------------------------------------------------------
// 回填内容与终止错误
// ---------------------------------------------------------------------------

// TestTurnLoopGuard_ToolResultWithHint 验证回填内容:未命中保持原样;命中则保留原始结果并追加提示。
func TestTurnLoopGuard_ToolResultWithHint(t *testing.T) {
	const result = `{"error":"unknown tool: read_file"}`

	if got := ToolResultWithHint(result, pCommon.BlankString); got != result {
		t.Errorf("未命中时工具结果不应被改写, got %q", got)
	}

	g := NewTurnLoopGuard()
	hint := pCommon.BlankString
	for i := 1; i <= maxIdenticalToolCallings; i++ {
		hint = g.Check("read_file", `{"path":"a.txt"}`, true, result)
	}
	if strings.TrimSpace(hint) == pCommon.BlankString {
		t.Fatal("连续 3 次完全相同调用应命中守卫")
	}

	got := ToolResultWithHint(result, hint)
	if !strings.HasPrefix(got, result) {
		t.Errorf("回填内容必须保留原始工具结果, got %q", got)
	}
	if !strings.Contains(got, hint) {
		t.Errorf("回填内容应包含守卫提示, got %q", got)
	}
}

// TestTurnLoopGuard_StopErrorIsChatKind 验证终止错误自带聊天域 Kind(可被 LlmError 原样透传给前端)。
func TestTurnLoopGuard_StopErrorIsChatKind(t *testing.T) {
	g := NewTurnLoopGuard()
	for i := 1; i <= maxIdenticalToolCallings; i++ {
		g.Check("read_file", `{"path":"a.txt"}`, true, "same")
	}

	err := g.StopError("chat service: test")
	if kind := apperr.Of(err); kind != myException.ChatToolLoopGuardKind {
		t.Fatalf("错误 Kind = %v, want %s", kind, myException.ChatToolLoopGuardKind.Name)
	}
	if !strings.Contains(err.Error(), loopGuardHintMarker) {
		t.Errorf("错误细节应带上守卫提示, got %v", err)
	}
}

// TestTurnLoopGuard_LastVerdict 验证最近一次命中的结构化判定可被读取(日志与用户提示分级依赖它)。
func TestTurnLoopGuard_LastVerdict(t *testing.T) {
	g := NewTurnLoopGuard()

	// 连续失败(规则 2,Warn 级)
	for i := 1; i <= maxConsecutiveCallErrors; i++ {
		g.Check(fmt.Sprintf("tool_%d", i), fmt.Sprintf(`{"n":%d}`, i), false, fmt.Sprintf("err-%d", i))
	}
	if v := g.LastVerdict(); v.Rule != LoopRuleConsecutiveErrors || v.Level != LoopLevelWarn {
		t.Fatalf("LastVerdict = %+v, want 规则 2/Warn", v)
	}

	// 完全相同(规则 4,Stop 级):应刷新为最新一次命中
	for i := 1; i <= maxIdenticalToolCallings; i++ {
		g.Check("read_file", `{"path":"a.txt"}`, true, "same")
	}
	if v := g.LastVerdict(); v.Rule != LoopRuleIdenticalCallings || v.Level != LoopLevelStop {
		t.Fatalf("LastVerdict = %+v, want 规则 4/Stop", v)
	}
}
