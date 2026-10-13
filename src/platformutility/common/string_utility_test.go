package common

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestTruncateRunesNoTruncation 未超长/不限制时原样返回,且**不追加** marker。
func TestTruncateRunesNoTruncation(t *testing.T) {
	const s = "hello world"

	cases := []struct {
		name   string
		max    int
		marker string
	}{
		{"远小于上限", len(s) + 10, "…"},
		{"恰好等于上限", len(s), "…"},
		{"max<=0 表示不限制", 0, "…"},
		{"负数同样视为不限制", -5, "…"},
	}
	for _, c := range cases {
		if got := TruncateRunes(s, c.max, c.marker); got != s {
			t.Errorf("%s: 不应截断, got %q", c.name, got)
		}
	}
}

// TestTruncateRunesWithMarker 发生截断时追加 marker(结果 = max 个 rune + marker)。
func TestTruncateRunesWithMarker(t *testing.T) {
	s := strings.Repeat("a", 100)

	got := TruncateRunes(s, 10, "…")
	if want := strings.Repeat("a", 10) + "…"; got != want {
		t.Errorf("带 marker 截断结果不正确:\n got %q\nwant %q", got, want)
	}
	if n := utf8.RuneCountInString(got); n != 11 {
		t.Errorf("结果应为 max+1 个 rune(marker 占 1), 实际 %d", n)
	}

	// 空 marker = 纯截断(调用方的既有语义,如 t_turn_tool.tool_resp 的长度上限)
	if got := TruncateRunes(s, 10, BlankString); got != strings.Repeat("a", 10) {
		t.Errorf("空 marker 应为纯截断, 实际 %q", got)
	}
}

// TestTruncateRunesCJK 多字节字符必须按 rune 边界截断,不得切出乱码。
func TestTruncateRunesCJK(t *testing.T) {
	s := "中文中文中文" // 6 个 rune / 18 字节

	// 字节长度超过 max 但 rune 数未超:不应截断(走 rune 数判断分支)
	if got := TruncateRunes(s, 10, "…"); got != s {
		t.Errorf("rune 数未超上限时不应截断: %q", got)
	}

	got := TruncateRunes(s, 3, "…")
	if want := "中文中…"; got != want {
		t.Errorf("CJK 截断结果不正确:\n got %q\nwant %q", got, want)
	}
	if !utf8.ValidString(got) {
		t.Errorf("截断结果不是合法 UTF-8(切断了多字节字符): %q", got)
	}
	if n := utf8.RuneCountInString(got); n != 4 {
		t.Errorf("结果应为 3 个 rune + marker, 实际 %d", n)
	}
}

// TestTruncateRunesBoundary 边界:恰好 max 个 rune 不截断,max+1 才截断。
func TestTruncateRunesBoundary(t *testing.T) {
	exact := "中文中文" // 4 rune
	if got := TruncateRunes(exact, 4, "…"); got != exact {
		t.Errorf("恰好达到上限不应截断: %q", got)
	}

	over := "中文中文中" // 5 rune
	if got := TruncateRunes(over, 4, "…"); got != "中文中文…" {
		t.Errorf("超出上限应截断到 max + marker: %q", got)
	}
}

// TestTruncateRunesEmpty 空串与 marker 混用的退化情况。
func TestTruncateRunesEmpty(t *testing.T) {
	if got := TruncateRunes(BlankString, 5, "…"); got != BlankString {
		t.Errorf("空串应原样返回: %q", got)
	}
	// 截断到 0 个 rune 时(仅 marker)—— 由 max<=0 的"不限制"语义覆盖,这里锁定行为:
	if got := TruncateRunes("abc", 0, "…"); got != "abc" {
		t.Errorf("max=0 表示不限制, 实际 %q", got)
	}
}

// TestNormalizeStringsSet 名单归一化:去空白/去空项/去重 + key 统一小写,
// 空名单或纯空白名单返回 nil(调用方据此判断"未声明名单")。
//
// 该覆盖原先落在 harness/common 的 NormalNameSet 上;该函数已被 NormalizeStringsSet 取代,
// 测试一并迁到函数所在包,避免重构静默丢失覆盖。
func TestNormalizeStringsSet(t *testing.T) {
	set := NormalizeStringsSet([]string{" Read ", "READ", "read", "", "   ", "Edit"})
	if len(set) != 2 {
		t.Fatalf("set size mismatch: got=%d want=2 (%v)", len(set), set)
	}
	for _, key := range []string{"read", "edit"} {
		if _, ok := set[key]; !ok {
			t.Errorf("normalized key %q should be present: %v", key, set)
		}
	}

	if got := NormalizeStringsSet(nil); got != nil {
		t.Errorf("nil input should return nil set, got=%v", got)
	}
	if got := NormalizeStringsSet([]string{"", "  "}); got != nil {
		t.Errorf("blank-only input should return nil set, got=%v", got)
	}
}
