/**
* LoopGuard 单元测试
*
* 目标:逐条验证 loop_guard.go 中四条判定规则及计数器复位的准确性。
*
* 被测规则与阈值:
*   1. 工具调用总量      maxToolTotalCallings     = 800
*   2. 连续调用失败次数  maxConsecutiveCallErrors = 5
*   3. 同一工具连续调用  maxSameToolCallings     = 10
*   4. 完全相同的调用    maxIdenticalToolCallings = 3
*   5. 二元交替的调用    maxAlternatingToolCallings = 3(轮,即 A,B,A,B,A,B 共 6 次调用)
*
* 说明:测试某一规则时,其余规则通过"每次调用使用不同的工具名/参数/结果"来隔离,
* 避免相互干扰;仅在需要验证规则交互时才刻意构造重叠场景。
**/

package security

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	pCommon "my.favorite/platformutility/common"
)

// variedCall 生成第 i 次调用(从 1 开始)的参数:工具名、参数、结果均不同,
// 用于隔离"同一工具连续调用"与"完全相同的调用"两条规则。
func variedCall(i int) (toolName, toolArgs string, resultOK bool, result string) {
	return fmt.Sprintf("tool_%d", i), fmt.Sprintf(`{"n":%d}`, i), true, fmt.Sprintf("result-%d", i)
}

// failedCall 同 variedCall,但返回失败结果,用于触发"连续调用失败"规则。
func failedCall(i int) (toolName, toolArgs string, resultOK bool, result string) {
	return fmt.Sprintf("tool_%d", i), fmt.Sprintf(`{"n":%d}`, i), false, fmt.Sprintf("error-%d", i)
}

// runUntilStop 反复调用 Check,返回触发停止时的调用序号与提示信息;
// 在 maxCalls 次内未触发则返回 (0, "")。
func runUntilStop(g *LoopGuard, maxCalls int, makeCall func(i int) (string, string, bool, string)) (int, string) {
	for i := 1; i <= maxCalls; i++ {
		toolName, toolArgs, resultOK, result := makeCall(i)
		if verdict := g.Check(toolName, toolArgs, resultOK, result); verdict.Hit() {
			return i, verdict.Msg
		}
	}

	return 0, ""
}

// ---------------------------------------------------------------------------
// 构造函数:初始状态
// ---------------------------------------------------------------------------

// TestNewLoopGuard_InitialState 验证新建守卫器的各项计数均为零值。
func TestNewLoopGuard_InitialState(t *testing.T) {
	g := NewLoopGuard()
	if g == nil {
		t.Fatal("NewLoopGuard() 返回 nil")
	}

	if g.curToolTotalCallings != 0 {
		t.Errorf("curToolTotalCallings = %d, want 0", g.curToolTotalCallings)
	}
	if g.curConsecutiveCallErrors != 0 {
		t.Errorf("curConsecutiveCallErrors = %d, want 0", g.curConsecutiveCallErrors)
	}
	if g.curSameToolCallings != 0 {
		t.Errorf("curSameToolCallings = %d, want 0", g.curSameToolCallings)
	}
	if g.lastToolName != "" {
		t.Errorf("lastToolName = %q, want 空字符串", g.lastToolName)
	}
	if len(g.recentTools) != 0 {
		t.Errorf("len(recentTools) = %d, want 0", len(g.recentTools))
	}
}

// ---------------------------------------------------------------------------
// 规则 1:工具调用总量
// ---------------------------------------------------------------------------

// TestLoopGuard_TotalCallings 验证第 800 次调用触发停止,且提示信息正确。
func TestLoopGuard_TotalCallings(t *testing.T) {
	g := NewLoopGuard()

	stopAt, msg := runUntilStop(g, maxToolTotalCallings+200, variedCall)
	if stopAt != maxToolTotalCallings {
		t.Fatalf("触发于第 %d 次调用, want 第 %d 次 (msg=%q)", stopAt, maxToolTotalCallings, msg)
	}

	want := fmt.Sprintf(maxToolTotalCallingsStr, maxToolTotalCallings)
	if msg != want {
		t.Errorf("提示信息 = %q, want %q", msg, want)
	}
	if g.curToolTotalCallings != maxToolTotalCallings {
		t.Errorf("curToolTotalCallings = %d, want %d", g.curToolTotalCallings, maxToolTotalCallings)
	}
}

// TestLoopGuard_TotalCallingsBelowLimit 验证上限内(799 次)不触发停止,且每次调用都计入总数。
func TestLoopGuard_TotalCallingsBelowLimit(t *testing.T) {
	g := NewLoopGuard()

	stopAt, msg := runUntilStop(g, maxToolTotalCallings-1, variedCall)
	if stopAt != 0 {
		t.Fatalf("第 %d 次调用即触发停止 (msg=%q), want 上限内不触发", stopAt, msg)
	}
	if g.curToolTotalCallings != maxToolTotalCallings-1 {
		t.Errorf("curToolTotalCallings = %d, want %d", g.curToolTotalCallings, maxToolTotalCallings-1)
	}
}

// ---------------------------------------------------------------------------
// 规则 2:连续调用失败
// ---------------------------------------------------------------------------

// TestLoopGuard_ConsecutiveCallErrors 验证连续第 5 次失败触发停止,且提示信息正确。
func TestLoopGuard_ConsecutiveCallErrors(t *testing.T) {
	g := NewLoopGuard()

	// 工具名/参数/结果均不同,只可能命中"连续失败"规则
	stopAt, msg := runUntilStop(g, maxConsecutiveCallErrors+50, failedCall)
	if stopAt != maxConsecutiveCallErrors {
		t.Fatalf("触发于第 %d 次失败调用, want 第 %d 次 (msg=%q)", stopAt, maxConsecutiveCallErrors, msg)
	}

	want := fmt.Sprintf(maxConsecutiveCallErrorsStr, maxConsecutiveCallErrors)
	if msg != want {
		t.Errorf("提示信息 = %q, want %q", msg, want)
	}
}

// TestLoopGuard_ConsecutiveCallErrorsBelowLimit 验证连续 4 次失败不触发停止。
func TestLoopGuard_ConsecutiveCallErrorsBelowLimit(t *testing.T) {
	g := NewLoopGuard()

	stopAt, msg := runUntilStop(g, maxConsecutiveCallErrors-1, failedCall)
	if stopAt != 0 {
		t.Fatalf("第 %d 次失败调用即触发停止 (msg=%q), want 阈值内不触发", stopAt, msg)
	}
	if g.curConsecutiveCallErrors != maxConsecutiveCallErrors-1 {
		t.Errorf("curConsecutiveCallErrors = %d, want %d", g.curConsecutiveCallErrors, maxConsecutiveCallErrors-1)
	}
}

// TestLoopGuard_ConsecutiveErrorsResetOnSuccess 验证一次成功调用后,连续失败计数清零。
func TestLoopGuard_ConsecutiveErrorsResetOnSuccess(t *testing.T) {
	g := NewLoopGuard()

	// 先连续失败 4 次(未到阈值)
	for i := 1; i <= maxConsecutiveCallErrors-1; i++ {
		if g.Check(failedCall(i)).Hit() {
			t.Fatalf("第 %d 次失败调用不应触发停止", i)
		}
	}
	if g.curConsecutiveCallErrors != maxConsecutiveCallErrors-1 {
		t.Fatalf("curConsecutiveCallErrors = %d, want %d", g.curConsecutiveCallErrors, maxConsecutiveCallErrors-1)
	}

	// 一次成功调用 → 计数清零
	if g.Check(variedCall(100)).Hit() {
		t.Fatal("成功调用不应触发停止")
	}
	if g.curConsecutiveCallErrors != 0 {
		t.Fatalf("成功调用后 curConsecutiveCallErrors = %d, want 0", g.curConsecutiveCallErrors)
	}

	// 再连续失败 4 次,仍不应触发
	for i := 201; i <= 200+maxConsecutiveCallErrors-1; i++ {
		if g.Check(failedCall(i)).Hit() {
			t.Fatalf("计数复位后第 %d 次失败调用不应触发停止", i-200)
		}
	}

	// 第 5 次失败触发
	verdict := g.Check(failedCall(300))
	if !verdict.Hit() {
		t.Fatalf("计数复位后再次连续失败 %d 次应触发停止", maxConsecutiveCallErrors)
	}
	if want := fmt.Sprintf(maxConsecutiveCallErrorsStr, maxConsecutiveCallErrors); verdict.Msg != want {
		t.Errorf("提示信息 = %q, want %q", verdict.Msg, want)
	}
}

// ---------------------------------------------------------------------------
// 规则 3:同一工具连续调用
// ---------------------------------------------------------------------------

// TestLoopGuard_SameToolConsecutive 验证同一工具连续调用 10 次触发停止。
func TestLoopGuard_SameToolConsecutive(t *testing.T) {
	g := NewLoopGuard()

	// 参数与结果每次都不同,只可能命中"同一工具连续调用"规则
	stopAt, msg := runUntilStop(g, 100, func(i int) (string, string, bool, string) {
		return "read_file", fmt.Sprintf(`{"path":"p%d"}`, i), true, fmt.Sprintf("ok-%d", i)
	})
	if stopAt != maxSameToolCallings {
		t.Fatalf("触发于同一工具连续第 %d 次调用, want 第 %d 次 (msg=%q)", stopAt, maxSameToolCallings, msg)
	}

	want := fmt.Sprintf(maxSameToolCallingsStr, maxSameToolCallings, "read_file")
	if msg != want {
		t.Errorf("提示信息 = %q, want %q", msg, want)
	}
}

// TestLoopGuard_DifferentToolResetsSameToolCounter 验证切换到其它工具后,同一工具计数重新开始。
func TestLoopGuard_DifferentToolResetsSameToolCounter(t *testing.T) {
	g := NewLoopGuard()

	// 连续调用 tool_a 9 次(阈值内)
	for i := 1; i <= maxSameToolCallings-1; i++ {
		if g.Check("tool_a", fmt.Sprintf(`{"n":%d}`, i), true, fmt.Sprintf("a-%d", i)).Hit() {
			t.Fatalf("tool_a 连续第 %d 次调用不应触发停止", i)
		}
	}

	// 中间插入一次其它工具
	if g.Check("tool_b", `{"n":0}`, true, "b-0").Hit() {
		t.Fatal("切换到 tool_b 不应触发停止")
	}
	if g.curSameToolCallings != 1 {
		t.Errorf("切换工具后 curSameToolCallings = %d, want 1(新工具已计 1 次)", g.curSameToolCallings)
	}

	// 回到 tool_a 后重新计数,9 次内仍不应触发
	for i := 101; i <= 100+maxSameToolCallings-1; i++ {
		if g.Check("tool_a", fmt.Sprintf(`{"n":%d}`, i), true, fmt.Sprintf("a2-%d", i)).Hit() {
			t.Fatalf("tool_a 重新计数后第 %d 次调用不应触发停止", i-100)
		}
	}
}

// TestLoopGuard_SameToolCaseInsensitive 验证工具名大小写不同仍视为同一工具并累计计数。
func TestLoopGuard_SameToolCaseInsensitive(t *testing.T) {
	g := NewLoopGuard()

	for i := 1; i <= maxSameToolCallings-1; i++ {
		name := "Read_File"
		if i%2 == 0 {
			name = "read_file"
		}
		if g.Check(name, fmt.Sprintf(`{"p":%d}`, i), true, fmt.Sprintf("r-%d", i)).Hit() {
			t.Fatalf("同一工具(大小写不同)连续第 %d 次调用不应触发停止", i)
		}
	}

	verdict := g.Check("READ_FILE", `{"p":999}`, true, "r-999")
	if !verdict.Hit() {
		t.Fatalf("同一工具(大小写不同)连续第 %d 次调用应触发停止", maxSameToolCallings)
	}
	if want := fmt.Sprintf(maxSameToolCallingsStr, maxSameToolCallings, "READ_FILE"); verdict.Msg != want {
		t.Errorf("提示信息 = %q, want %q", verdict.Msg, want)
	}
}

// ---------------------------------------------------------------------------
// 规则 4:完全相同的调用
// ---------------------------------------------------------------------------

// TestLoopGuard_IdenticalToolCallings 验证第 3 次完全相同(工具名+参数+结果)的调用触发停止。
func TestLoopGuard_IdenticalToolCallings(t *testing.T) {
	g := NewLoopGuard()

	for i := 1; i < maxIdenticalToolCallings; i++ {
		if g.Check("search", `{"q":"abc"}`, true, "same-result").Hit() {
			t.Fatalf("相同调用第 %d 次不应触发停止", i)
		}
	}

	verdict := g.Check("search", `{"q":"abc"}`, true, "same-result")
	if !verdict.Hit() {
		t.Fatalf("完全相同调用第 %d 次应触发停止", maxIdenticalToolCallings)
	}

	want := fmt.Sprintf(maxIdenticalToolCallingMsg, maxIdenticalToolCallings, "search")
	if verdict.Msg != want {
		t.Errorf("提示信息 = %q, want %q", verdict.Msg, want)
	}
}

// TestLoopGuard_IdenticalMustBeConsecutive 验证"完全相同"必须是连续的:中间被不同调用打断后重新计数。
func TestLoopGuard_IdenticalMustBeConsecutive(t *testing.T) {
	g := NewLoopGuard()

	seq := []struct {
		args   string
		result string
	}{
		{`{"q":"x"}`, "r1"}, // 相同 1
		{`{"q":"x"}`, "r1"}, // 相同 2
		{`{"q":"y"}`, "r2"}, // 打断
		{`{"q":"x"}`, "r1"}, // 相同 1
		{`{"q":"x"}`, "r1"}, // 相同 2
	}

	for i, s := range seq {
		if g.Check("search", s.args, true, s.result).Hit() {
			t.Fatalf("第 %d 次调用不应触发停止(相同调用被中途打断)", i+1)
		}
	}
}

// TestLoopGuard_IdenticalStaysTriggered 验证一旦判定为死循环,后续相同调用仍会持续判定。
func TestLoopGuard_IdenticalStaysTriggered(t *testing.T) {
	g := NewLoopGuard()

	for i := 1; i <= maxIdenticalToolCallings; i++ {
		g.Check("search", `{"q":"x"}`, true, "same")
	}

	if !g.Check("search", `{"q":"x"}`, true, "same").Hit() {
		t.Fatal("死循环持续期间的相同调用仍应判定为死循环")
	}
}

// TestLoopGuard_EmptyArgsAndResult 验证参数与结果均为空时不 panic,且按相同调用累计判定。
func TestLoopGuard_EmptyArgsAndResult(t *testing.T) {
	g := NewLoopGuard()

	for i := 1; i < maxIdenticalToolCallings; i++ {
		if g.Check("noop", "", true, "").Hit() {
			t.Fatalf("空参数空结果第 %d 次调用不应触发停止", i)
		}
	}

	if !g.Check("noop", "", true, "").Hit() {
		t.Fatalf("空参数空结果第 %d 次调用应触发停止", maxIdenticalToolCallings)
	}
}

// TestLoopGuard_IdenticalCallDifferentResult 记录当前实现的判定口径:
// 签名 = 工具名 + 参数哈希 + 结果前 200 字符,因此"参数完全相同但结果每次不同"
// 不会被判定为死循环(例如带时间戳的轮询输出)。
func TestLoopGuard_IdenticalCallDifferentResult(t *testing.T) {
	g := NewLoopGuard()

	// 同一工具、同一参数,结果每次不同;次数控制在"同一工具连续调用"阈值内以隔离规则 3
	for i := 1; i <= maxSameToolCallings-1; i++ {
		if g.Check("status", `{"id":1}`, true, fmt.Sprintf("running at %d", i)).Hit() {
			t.Fatalf("第 %d 次调用被判定为停止,但当前实现会因结果不同而不判定为相同调用", i)
		}
	}
}

// ---------------------------------------------------------------------------
// 规则 5:二元交替调用(A,B,A,B,...)
// ---------------------------------------------------------------------------

// TestLoopGuard_AlternatingToolCallings 验证 A、B 交替重复 3 轮(第 6 次调用)时触发停止。
func TestLoopGuard_AlternatingToolCallings(t *testing.T) {
	g := NewLoopGuard()

	stopAt, msg := runUntilStop(g, 100, func(i int) (string, string, bool, string) {
		if i%2 == 1 {
			return "read_file", `{"path":"a.txt"}`, true, "content-a"
		}

		return "run_cmd", `{"cmd":"ls"}`, true, "cmd-out"
	})
	if stopAt != maxAlternatingToolCallings*2 {
		t.Fatalf("触发于第 %d 次调用, want 第 %d 次 (msg=%q)", stopAt, maxAlternatingToolCallings*2, msg)
	}

	want := fmt.Sprintf(maxAlternatingToolCallingMsg, maxAlternatingToolCallings, "read_file", "run_cmd")
	if msg != want {
		t.Errorf("提示信息 = %q, want %q", msg, want)
	}
}

// TestLoopGuard_AlternatingToolCallingsBelowLimit 验证只交替 2 轮(4 次调用)时不触发停止。
func TestLoopGuard_AlternatingToolCallingsBelowLimit(t *testing.T) {
	g := NewLoopGuard()

	stopAt, msg := runUntilStop(g, maxAlternatingToolCallings*2-2, func(i int) (string, string, bool, string) {
		if i%2 == 1 {
			return "read_file", `{"path":"a.txt"}`, true, "content-a"
		}

		return "run_cmd", `{"cmd":"ls"}`, true, "cmd-out"
	})
	if stopAt != 0 {
		t.Fatalf("第 %d 次调用即触发停止 (msg=%q), want 阈值内不触发", stopAt, msg)
	}
}

// TestLoopGuard_AlternatingMustBeConsecutive 验证交替必须是连续的:中途插入其它调用后需重新累计 3 轮。
func TestLoopGuard_AlternatingMustBeConsecutive(t *testing.T) {
	g := NewLoopGuard()

	// 先交替 2 轮半(A,B,A,B,A),再用一次无关调用打断
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
		{"list_dir", `{"path":"."}`, "dir-out"},
	}

	for i, s := range seq {
		if verdict := g.Check(s.name, s.args, true, s.result); verdict.Hit() {
			t.Fatalf("第 %d 次调用不应触发停止 (msg=%q)", i+1, verdict.Msg)
		}
	}

	// 打断之后重新连续交替 3 轮,应在第 12 次调用触发
	for i := 7; i <= 12; i++ {
		name, args, result := "read_file", `{"path":"a.txt"}`, "content-a"
		if i%2 == 0 {
			name, args, result = "run_cmd", `{"cmd":"ls"}`, "cmd-out"
		}

		verdict := g.Check(name, args, true, result)
		if i < 12 && verdict.Hit() {
			t.Fatalf("第 %d 次调用不应触发停止 (msg=%q)", i, verdict.Msg)
		}
		if i == 12 && !verdict.Hit() {
			t.Fatal("打断后重新连续交替 3 轮,第 12 次调用应触发停止")
		}
	}
}

// TestLoopGuard_AlternatingArgsOnly 验证同一工具、两组参数交替(工具名相同)也会被判定为二元交替,
// 并用同一工具专用的提示文案(否则会输出"在 read_file 与 read_file 之间交替"这种废话)。
func TestLoopGuard_AlternatingArgsOnly(t *testing.T) {
	g := NewLoopGuard()

	stopAt, msg := runUntilStop(g, 100, func(i int) (string, string, bool, string) {
		return "read_file", fmt.Sprintf(`{"path":"p%d.txt"}`, i%2), true, fmt.Sprintf("content-%d", i%2)
	})
	if stopAt != maxAlternatingToolCallings*2 {
		t.Fatalf("触发于第 %d 次调用, want 第 %d 次 (msg=%q)", stopAt, maxAlternatingToolCallings*2, msg)
	}

	want := fmt.Sprintf(maxAlternatingSameToolCallingMsg, maxAlternatingToolCallings, "read_file")
	if msg != want {
		t.Errorf("提示信息 = %q, want %q", msg, want)
	}
}

// TestLoopGuard_ReadEditReadNotStopped 验证"读→改→读"这类正常模式不会被误判:
// 二元交替需要 A、B 各自重复 3 次,3 次调用不足以判定。
func TestLoopGuard_ReadEditReadNotStopped(t *testing.T) {
	g := NewLoopGuard()

	seq := []struct {
		name   string
		args   string
		result string
	}{
		{"read_file", `{"path":"a.txt"}`, "content-a"},  //读
		{"edit_file", `{"path":"a.txt","v":1}`, "done"}, //改
		{"read_file", `{"path":"a.txt"}`, "content-a"},  //再读(验证)
		{"edit_file", `{"path":"a.txt","v":2}`, "done"}, //再改
		{"read_file", `{"path":"a.txt"}`, "content-a"},  //再读(验证)
		{"edit_file", `{"path":"a.txt","v":3}`, "done"}, //再改
	}

	for i, s := range seq {
		if verdict := g.Check(s.name, s.args, true, s.result); verdict.Hit() {
			t.Fatalf("第 %d 次调用被误判为停止 (msg=%q)", i+1, verdict.Msg)
		}
	}
}

// TestLoopGuard_ThreeToolsCycleStoppedByNoProgress 验证三个及以上工具的周期调用
// (A,B,C,A,B,C...)虽然不属于"二元交替"规则,但会被规则 6(无进展)捕获:
// 前 3 次各自是"新结果",之后连续 8 次结果都已在窗口内 → 第 11 次调用命中 no_progress。
func TestLoopGuard_ThreeToolsCycleStoppedByNoProgress(t *testing.T) {
	g := NewLoopGuard()

	const cycleTools = 3
	wantHitAt := cycleTools + progressMaxNoNewCalls // 3 + 8 = 11

	for i := 1; i <= 60; i++ {
		name := fmt.Sprintf("tool_%d", i%3)
		verdict := g.Check(name, fmt.Sprintf(`{"n":%d}`, i%3), true, fmt.Sprintf("out-%d", i%3))

		if i < wantHitAt {
			if verdict.Hit() {
				t.Fatalf("三工具周期调用第 %d 次即被判定为停止(早于预期第 %d 次), msg=%q", i, wantHitAt, verdict.Msg)
			}
			continue
		}

		if !verdict.Hit() {
			t.Fatalf("三工具周期调用第 %d 次应被无进展规则判定为停止", i)
		}
		if verdict.Rule != LoopRuleNoProgress {
			t.Fatalf("命中规则 = %s, want %s", verdict.Rule, LoopRuleNoProgress)
		}
		if verdict.Level != LoopLevelStop {
			t.Fatalf("命中级别 = %s, want %s", verdict.Level, LoopLevelStop)
		}
		return
	}

	t.Fatal("三工具周期调用在 60 次内始终未被判定为停止")
}

// ---------------------------------------------------------------------------
// 规则交互、边界与误报
// ---------------------------------------------------------------------------

// TestLoopGuard_NoFalsePositiveOnVariedCalls 验证正常的多样化调用不会被误判。
func TestLoopGuard_NoFalsePositiveOnVariedCalls(t *testing.T) {
	g := NewLoopGuard()

	tools := []string{"read_file", "write_file", "search", "run_cmd", "list_dir"}
	for i := 1; i <= 300; i++ {
		name := tools[i%len(tools)]
		if verdict := g.Check(name, fmt.Sprintf(`{"i":%d}`, i), true, fmt.Sprintf("out-%d", i)); verdict.Hit() {
			t.Fatalf("第 %d 次正常调用被误判为停止 (msg=%q)", i, verdict.Msg)
		}
	}
}

// TestLoopGuard_RecentToolsBounded 验证 recentTools 窗口不会无限增长。
func TestLoopGuard_RecentToolsBounded(t *testing.T) {
	g := NewLoopGuard()

	for i := 1; i <= 500; i++ {
		g.Check(variedCall(i))
	}

	if len(g.recentTools) > 10 {
		t.Errorf("len(recentTools) = %d, want <= 10", len(g.recentTools))
	}
}

// TestLoopGuard_StopMessageNotBlank 验证未触发时返回空消息与 false。
func TestLoopGuard_StopMessageNotBlank(t *testing.T) {
	g := NewLoopGuard()

	verdict := g.Check("read_file", `{"path":"a.txt"}`, true, "content")
	if verdict.Hit() {
		t.Fatalf("首次调用不应触发停止 (msg=%q)", verdict.Msg)
	}
	if strings.TrimSpace(verdict.Msg) != "" {
		t.Errorf("未触发时消息应为空, got %q", verdict.Msg)
	}
}

// ---------------------------------------------------------------------------
// 签名口径与状态更新顺序
// ---------------------------------------------------------------------------

// TestLoopGuard_SignatureTruncatesByRunes 验证签名按 rune 截断:
// 按字节截断会把多字节字符切坏(签名出现非法 UTF-8),且 200 字节对 CJK 只有 ~66 个字,窗口失真。
func TestLoopGuard_SignatureTruncatesByRunes(t *testing.T) {
	const longRunes = 250 // rune 数(750 字节,远超 200 字节)

	g := NewLoopGuard()
	g.Check("read_file", `{"path":"a.txt"}`, true, strings.Repeat("中", longRunes))

	if len(g.recentTools) != 1 {
		t.Fatalf("len(recentTools) = %d, want 1", len(g.recentTools))
	}
	if !utf8.ValidString(g.recentTools[0]) {
		t.Errorf("签名应为合法 UTF-8(按 rune 截断), got %q", g.recentTools[0])
	}

	// 第 200 个 rune 之后的差异不进签名
	outer := NewLoopGuard()
	outer.Check("read_file", `{"path":"a.txt"}`, true, strings.Repeat("中", longRunes-1)+"文")
	if g.recentTools[0] != outer.recentTools[0] {
		t.Errorf("第 %d 个 rune 之后的差异不应影响签名", maxSignatureResultRunes)
	}

	// 第 200 个 rune 之内的差异必须体现在签名里
	inner := NewLoopGuard()
	inner.Check("read_file", `{"path":"a.txt"}`, true, "文"+strings.Repeat("中", longRunes-1))
	if g.recentTools[0] == inner.recentTools[0] {
		t.Errorf("第 %d 个 rune 之内的差异必须体现在签名里", maxSignatureResultRunes)
	}
}

// TestLoopGuard_EmptyArgsHashPlaceholder 验证空参数用占位符,不让签名出现空白段(与"真实哈希"混淆)。
func TestLoopGuard_EmptyArgsHashPlaceholder(t *testing.T) {
	g := NewLoopGuard()
	g.Check("noop", pCommon.BlankString, true, "ok")

	if len(g.recentTools) != 1 {
		t.Fatalf("len(recentTools) = %d, want 1", len(g.recentTools))
	}
	if !strings.HasPrefix(g.recentTools[0], "noop:"+emptyToolArgsHash+":") {
		t.Errorf("空参数应使用占位符 %q, got %q", emptyToolArgsHash, g.recentTools[0])
	}
}

// TestLoopGuard_StateUpdatedBeforeRules 验证"先更新状态、再判定":
// 命中规则 2 的那次调用同样要计入「同一工具计数」与签名窗口。
// (旧实现是提前 return,这两项都漏记 ⇒ 编排层允许"命中后继续跑"时,后续判定基于过期状态。)
func TestLoopGuard_StateUpdatedBeforeRules(t *testing.T) {
	g := NewLoopGuard()
	for i := 1; i <= maxConsecutiveCallErrors; i++ {
		stop := g.Check("read_file", fmt.Sprintf(`{"n":%d}`, i), false, fmt.Sprintf("err-%d", i)).Hit()
		if wantStop := i == maxConsecutiveCallErrors; stop != wantStop {
			t.Fatalf("第 %d 次失败调用 stop=%v, want %v", i, stop, wantStop)
		}
	}

	if g.curSameToolCallings != maxConsecutiveCallErrors {
		t.Errorf("命中规则 2 的那次调用也应计入同一工具计数: curSameToolCallings = %d, want %d",
			g.curSameToolCallings, maxConsecutiveCallErrors)
	}
	if len(g.recentTools) != maxConsecutiveCallErrors {
		t.Errorf("命中规则 2 的那次调用也应进入签名窗口: len(recentTools) = %d, want %d",
			len(g.recentTools), maxConsecutiveCallErrors)
	}

	// 换成成功调用会把连续失败计数清零,但"同一工具计数"应接着往累计
	remain := maxSameToolCallings - maxConsecutiveCallErrors
	for i := 1; i <= remain; i++ {
		stop := g.Check("read_file", fmt.Sprintf(`{"n":%d}`, 100+i), true, fmt.Sprintf("ok-%d", i)).Hit()
		if wantStop := i == remain; stop != wantStop {
			t.Fatalf("命中后第 %d 次同一工具调用 stop=%v, want %v(计数不应从命中处断档)", i, stop, wantStop)
		}
	}
}

// ---------------------------------------------------------------------------
// 结构化判定(规则 / 级别 / 提示)
// ---------------------------------------------------------------------------

// TestLoopGuard_VerdictRuleAndLevel 验证每条规则的结构化判定:
// "换一种方法/方式"的规则是 Warn(可继续),"停止重试/总结并停止"的规则是 Stop。
func TestLoopGuard_VerdictRuleAndLevel(t *testing.T) {
	cases := []struct {
		name  string
		rule  LoopGuardRule
		level LoopGuardLevel
		drive func(g *LoopGuard) LoopGuardVerdict
	}{
		{
			name: "规则 2 连续失败 → Warn",
			rule: LoopRuleConsecutiveErrors, level: LoopLevelWarn,
			drive: func(g *LoopGuard) LoopGuardVerdict {
				var v LoopGuardVerdict
				for i := 1; i <= maxConsecutiveCallErrors; i++ {
					v = g.Check(fmt.Sprintf("tool_%d", i), fmt.Sprintf(`{"n":%d}`, i), false, fmt.Sprintf("err-%d", i))
				}
				return v
			},
		},
		{
			name: "规则 3 同一工具连续 → Warn",
			rule: LoopRuleSameToolCallings, level: LoopLevelWarn,
			drive: func(g *LoopGuard) LoopGuardVerdict {
				var v LoopGuardVerdict
				for i := 1; i <= maxSameToolCallings; i++ {
					v = g.Check("read_file", fmt.Sprintf(`{"p":%d}`, i), true, fmt.Sprintf("r-%d", i))
				}
				return v
			},
		},
		{
			name: "规则 4 完全相同 → Stop",
			rule: LoopRuleIdenticalCallings, level: LoopLevelStop,
			drive: func(g *LoopGuard) LoopGuardVerdict {
				var v LoopGuardVerdict
				for i := 1; i <= maxIdenticalToolCallings; i++ {
					v = g.Check("search", `{"q":"x"}`, true, "same")
				}
				return v
			},
		},
		{
			name: "规则 5 二元交替 → Stop",
			rule: LoopRuleAlternatingCallings, level: LoopLevelStop,
			drive: func(g *LoopGuard) LoopGuardVerdict {
				var v LoopGuardVerdict
				for i := 1; i <= maxAlternatingToolCallings*2; i++ {
					if i%2 == 1 {
						v = g.Check("read_file", `{"p":"a"}`, true, "ra")
						continue
					}
					v = g.Check("run_cmd", `{"c":"ls"}`, true, "rc")
				}
				return v
			},
		},
		{
			name: "规则 1 调用总量 → Stop",
			rule: LoopRuleTotalCallings, level: LoopLevelStop,
			drive: func(g *LoopGuard) LoopGuardVerdict {
				var v LoopGuardVerdict
				for i := 1; i <= maxToolTotalCallings; i++ {
					v = g.Check(fmt.Sprintf("tool_%d", i), fmt.Sprintf(`{"n":%d}`, i), true, fmt.Sprintf("r-%d", i))
				}
				return v
			},
		},
	}

	for _, c := range cases {
		v := c.drive(NewLoopGuard())
		if !v.Hit() {
			t.Fatalf("%s: 未命中", c.name)
		}
		if v.Rule != c.rule {
			t.Errorf("%s: Rule = %v, want %v", c.name, v.Rule, c.rule)
		}
		if v.Level != c.level {
			t.Errorf("%s: Level = %v, want %v", c.name, v.Level, c.level)
		}
		if strings.TrimSpace(v.Msg) == "" {
			t.Errorf("%s: 提示不应为空", c.name)
		}
	}

	// 未命中:零值判定(编排层用 Hit() 判定,不应依赖 Msg 文本)
	if v := NewLoopGuard().Check("read_file", `{"p":"a"}`, true, "r"); v.Hit() || v.Rule != LoopRuleNone || v.Level != LoopLevelNone || v.Msg != "" {
		t.Errorf("未命中时应为零值判定, got %+v", v)
	}
}

// TestLoopGuard_VerdictStringNames 验证日志/指标用的稳定名称(改动会让线上日志口径漂移)。
func TestLoopGuard_VerdictStringNames(t *testing.T) {
	if got := LoopRuleIdenticalCallings.String(); got != "identical_callings" {
		t.Errorf("LoopRuleIdenticalCallings.String() = %s, want identical_callings", got)
	}
	if got := LoopRuleAlternatingCallings.String(); got != "alternating_callings" {
		t.Errorf("LoopRuleAlternatingCallings.String() = %s, want alternating_callings", got)
	}
	if got := LoopLevelWarn.String(); got != "warn" {
		t.Errorf("LoopLevelWarn.String() = %s, want warn", got)
	}
	if got := LoopLevelStop.String(); got != "stop" {
		t.Errorf("LoopLevelStop.String() = %s, want stop", got)
	}
	if got := LoopRuleNone.String(); got != "none" {
		t.Errorf("LoopRuleNone.String() = %s, want none", got)
	}
	if got := LoopLevelNone.String(); got != "none" {
		t.Errorf("LoopLevelNone.String() = %s, want none", got)
	}
}
