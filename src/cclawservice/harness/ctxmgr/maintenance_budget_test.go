/**
* 维护调用(蒸馏/压缩/合并)的窗口预算 —— F11
*
* 为什么单独锁:维护调用同样占模型窗口,而它们的源文本上限是"看着大模型定的"常量。
* 换成小窗模型时若只按常量发,**后台会静默失败**(日志之外没有任何信号),
* 表现为"记忆不再更新"。
**/

package ctxmgr

import (
	"strings"
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/llmproto"
)

const maintenanceTestSystemPrompt = "你是记忆维护助手"

// maintenanceTestInputTokens 估算"系统提示 + 用户提示"的输入 token(用例里用它反推窗口)。
func maintenanceTestInputTokens(userPrompt string) int {
	return EstimateInputTokens([]llmproto.ChatMessage{
		{Role: hCommon.RoleSystem, Content: maintenanceTestSystemPrompt},
		{Role: hCommon.RoleUser, Content: userPrompt},
	})
}

// TestMaintenanceInputBudgetFollowsWindow 源文本预算 = min(硬上限, 窗口扣除输出与系统提示后的余额)。
func TestMaintenanceInputBudgetFollowsWindow(t *testing.T) {
	const hardCap = hCommon.DistillMaxInputChars
	const output = hCommon.SummaryMaxTokens

	systemTokens := EstimateInputTokens([]llmproto.ChatMessage{{Role: hCommon.RoleSystem, Content: maintenanceTestSystemPrompt}})
	// 余额 1000 token(≈1500 rune,小于硬上限)→ 预算应被窗口收窄到该值
	tight := output + systemTokens + 1000

	cases := []struct {
		name   string
		window int
		want   int
	}{
		{"窗口未知:沿用硬上限(与\"不配置窗口就不裁剪\"同一口径)", 0, hardCap},
		{"大窗口:硬上限才是瓶颈", 1 << 20, hardCap},
		{"小窗口:按窗口收窄", tight, TokensToChars(1000)},
		{"窗口只够输出额度:返回 0(调用方跳过)", output + systemTokens - 1, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := calcMaintenanceInputBudgetRunes(c.window, output, maintenanceTestSystemPrompt, hardCap)
			if got != c.want {
				t.Fatalf("预算=%d,期望 %d (window=%d)", got, c.want, c.window)
			}
			if got > hardCap {
				t.Errorf("预算不得突破硬上限 %d: %d", hardCap, got)
			}
			if c.window == tight && got >= hardCap {
				t.Errorf("小窗口下预算必须真的收紧: %d", got)
			}
		})
	}
}

// TestMaintenanceInputBudgetIsConservative 换算方向必须保守:宁可少发也不超窗。
func TestMaintenanceInputBudgetIsConservative(t *testing.T) {
	// 最坏情况(全中文)按 1.5 rune/token;若按 ASCII 的 4 rune/token 换算,
	// 中文内容会把请求顶出窗口。
	if got := TokensToChars(1000); got != 1500 {
		t.Fatalf("TokensToChars 应按最坏情况(CJK 1.5 rune/token),实际 %d", got)
	}
	if got := TokensToChars(0); got != 0 {
		t.Fatalf("非正额度应返回 0,实际 %d", got)
	}

	// 不变量:预算换算回 token 后,再加上输出额度与系统提示,不得超过窗口
	const output = hCommon.SummaryMaxTokens
	systemTokens := EstimateInputTokens([]llmproto.ChatMessage{{Role: hCommon.RoleSystem, Content: maintenanceTestSystemPrompt}})
	window := output + systemTokens + 1000

	budget := calcMaintenanceInputBudgetRunes(window, output, maintenanceTestSystemPrompt, hCommon.DistillMaxInputChars)
	if used := CharsToTokens(budget) + output + systemTokens; used > window {
		t.Errorf("预算 + 输出 + 系统提示 = %d,超出窗口 %d", used, window)
	}
}

// TestMaintenanceOutputTokensFollowsWindow 输出额度 = min(维护输出上限, 窗口余额),装不下则 0。
func TestMaintenanceOutputTokensFollowsWindow(t *testing.T) {
	const maxTokens = hCommon.SummaryMaxTokens
	longPrompt := strings.Repeat("历史记录", 3000) // ~12000 rune

	need := maintenanceTestInputTokens(longPrompt)
	// 余额 = MaintenanceMinOutputTokens + 100:落在 [维护最小额度, 维护输出上限) 之间 → 按余额收窄
	tight := need + hCommon.MaintenanceMinOutputTokens + 100

	cases := []struct {
		name   string
		window int
		want   int
	}{
		{"窗口未知:沿用维护输出上限", 0, maxTokens},
		{"大窗口:输出上限才是瓶颈", 1 << 20, maxTokens},
		{"窗口紧张:按余额收窄", tight, tight - need},
		{"窗口装不下:返回 0(不发起必然失败的请求)", need - 100, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CalcMaintenanceOutputTokens(c.window, maxTokens, maintenanceTestSystemPrompt, longPrompt)
			if got != c.want {
				t.Fatalf("输出额度=%d,期望 %d (window=%d, 输入估量=%d)", got, c.want, c.window, need)
			}
			if got > maxTokens {
				t.Errorf("不得突破维护输出上限 %d: %d", maxTokens, got)
			}
			// 核心不变量:输出额度 + 估算输入 ≤ 窗口(窗口已知且确实发起调用时)
			if c.window > 0 && got > 0 && got+need > c.window {
				t.Errorf("输出额度 + 估算输入 = %d,超出窗口 %d", got+need, c.window)
			}
		})
	}
}

// TestPolicyWarnsWhenWindowCannotFitMaintenance 装不下维护调用时必须有一条指向配置的告警
// (否则运维只能看到"记忆不再更新",而日志里只有 debug 级跳过记录)。
func TestPolicyWarnsWhenWindowCannotFitMaintenance(t *testing.T) {
	contains := func(warnings []string, keyword string) bool {
		for _, w := range warnings {
			if strings.Contains(w, keyword) {
				return true
			}
		}
		return false
	}

	// 窗口 <= 维护输出额度 + 系统提示:维护调用一点空间都没有(avail <= 0)。
	// 历史侧放开,确保只有维护那条告警。
	tiny := PolicyWarnings(ContextPolicy{
		ContextWindow:    hCommon.SummaryMaxTokens,
		OutputReserve:    hCommon.DefaultOutputReserveTokens,
		MaxHistoryTokens: 1 << 20,
	}, 100)
	if !contains(tiny, "no room for a maintenance call") {
		t.Errorf("窗口装不下维护调用时应告警,实际 %v", tiny)
	}

	roomy := PolicyWarnings(ContextPolicy{
		ContextWindow:    1 << 20,
		OutputReserve:    hCommon.DefaultOutputReserveTokens,
		MaxHistoryTokens: hCommon.DefaultMaxHistoryTokens,
	}, 100)
	if contains(roomy, "no room for a maintenance call") {
		t.Errorf("大窗口不应产生该告警,实际 %v", roomy)
	}
}
