/**
* ForModel 的"回填口径"契约测试。
*
* 重点锁定一条曾经静默失效的行为:确定性失败(沙箱/规则拒绝)必须把"不要再试"的
* 提示**送到模型面前**,而且不能被正文截断吃掉 —— 提示在尾部,截断保留的是头部。
**/

package chat

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/harness/toolexecutor"
)

const forModelTestReason = "operation not permitted: sandbox denied write"

// TestForModel_NoRetryHintVisible 放得下正文时,提示必须出现在结尾。
func TestForModel_NoRetryHintVisible(t *testing.T) {
	o := toolexecutor.ToolOutcome{Name: "write", Reason: errors.New(forModelTestReason)}

	got := ForModel(o, 1200)
	if !strings.HasSuffix(got, noRetryHint) {
		t.Fatalf("确定性失败必须附带提示,实际结尾: %q", tail(got, 80))
	}
}

// TestForModel_NoRetryHintSurvivesTruncation 正文超限时,提示仍必须完整保留。
//
// 这是原实现的缺陷:先拼接提示、再按 maxRunes 从头部截断,提示永远被切掉,
// 模型看不到"不要重试",于是反复发起同样的调用。
func TestForModel_NoRetryHintSurvivesTruncation(t *testing.T) {
	const maxRunes = 200

	o := toolexecutor.ToolOutcome{
		Name:   "bash",
		Reason: errors.New(forModelTestReason + strings.Repeat("甲", 5000)),
	}

	got := ForModel(o, maxRunes)

	if !strings.HasSuffix(got, noRetryHint) {
		t.Fatalf("超长正文下提示被截断吃掉了,实际结尾: %q", tail(got, 80))
	}

	if n := utf8.RuneCountInString(got); n > maxRunes {
		t.Fatalf("回填文本超过上限: %d > %d", n, maxRunes)
	}

	if !strings.Contains(got, cCommon.TruncatedMarker) {
		t.Fatalf("正文被截断时应带省略号: %q", tail(got, 120))
	}
}

// TestForModel_PlainResultNoHint 非确定性失败(SDK 普通错误)不该被加提示。
func TestForModel_PlainResultNoHint(t *testing.T) {
	o := toolexecutor.ToolOutcome{Name: "read", Full: "hello", Reason: errors.New("connection reset by peer")}

	got := ForModel(o, 1200)
	if strings.Contains(got, "[提示]") {
		t.Fatalf("不可重试白名单之外的失败不应加提示: %q", got)
	}
}

// TestForModel_Unlimited 极长正文且 maxRunes<=0 时不截断,但仍带提示。
func TestForModel_Unlimited(t *testing.T) {
	longMsg := forModelTestReason + strings.Repeat("乙", 3000)
	o := toolexecutor.ToolOutcome{Name: "bash", Reason: errors.New(longMsg)}

	got := ForModel(o, 0)
	if !strings.Contains(got, longMsg) {
		t.Fatal("maxRunes<=0 表示不限长,正文应原样保留")
	}

	if !strings.HasSuffix(got, noRetryHint) {
		t.Fatal("不限长时提示同样必须存在")
	}
}

// TestForModel_ReasonRecordedAsJSON 失败结果在正文里仍是 JSON 形态(历史口径不变)。
func TestForModel_ReasonRecordedAsJSON(t *testing.T) {
	o := toolexecutor.ToolOutcome{Name: "read", Reason: errors.New("boom")}

	got := ForModel(o, 1200)
	if !strings.Contains(got, `{"error":"boom"}`) {
		t.Fatalf("失败正文口径被改变: %q", got)
	}
}

// cTruncatedMarker 复制自 common.TruncatedMarker 的断言用常量(避免测试依赖具体字符)。
// tail 返回字符串末尾 n 个 rune,便于失败信息可读。
func tail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}

	return string(r[len(r)-n:])
}
