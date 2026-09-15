/**
* 历史回放上限(R2)、工具 schema 预留(R3)与预算分解可观测性(R4)的契约
*
* 这一组锁住"窗口这么大,但历史只回放这么多"的三件事:
*   - 上限只作用于历史回放,不作用于工具循环;
*   - 上限不会制造 BudgetExhausted(那是配置问题,不是上限引起的);
*   - 预算分解必须能直接读到,便于日志/告警排查。
**/

package ctxmgr

import (
	"fmt"
	"strings"
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/llmproto"
	"my.favorite/cclawservice/model/dataobject"
)

// compactResult 只打印计数(AssembleResult 里带着全部消息,直接 %+v 会把日志刷爆)。
func compactResult(r AssembleResult) string {
	return fmt.Sprintf("trimmed=%v kept=%d droppedRecords=%d budget=%d cap=%d system=%d tools=%d exhausted=%v",
		r.Trimmed, len(r.Messages), r.DroppedRecordCount, r.HistoryBudgetTokens, r.HistoryCapTokens,
		r.SystemReserveTokens, r.ToolsReserveTokens, r.BudgetExhausted)
}

// capRecords 造 n 条等长记录(每条约 300 rune → 约 80 token)。
func capRecords(n int) []hCommon.MemoryRecord {
	body := strings.Repeat("内容说明", 75)
	records := make([]hCommon.MemoryRecord, 0, n)
	for i := 1; i <= n; i++ {
		records = append(records, record(i, hCommon.HistoryRecordTypeUser, "", body))
	}
	return records
}

// TestHistoryCapLimitsReplayOnly 上限把历史回放收窄,但同一策略下工具循环不受影响。
func TestHistoryCapLimitsReplayOnly(t *testing.T) {
	records := capRecords(200) // 约 6 万 rune ≈ 1.6 万 token
	system := "SYS"

	largeWindow := 200000
	uncapped := ContextPolicy{ContextWindow: largeWindow, OutputReserve: 1000, MaxHistoryTokens: -1}
	capped := uncapped
	capped.MaxHistoryTokens = 4000

	wide := AssembleContext(system, records, "问题", uncapped)
	narrow := AssembleContext(system, records, "问题", capped)

	if !narrow.Trimmed || narrow.DroppedRecordCount == 0 {
		t.Fatalf("设了上限就应该裁掉历史: %s", compactResult(narrow))
	}
	if wide.DroppedRecordCount != 0 {
		t.Fatalf("不设上限时窗口足够,不应裁剪: %s", compactResult(wide))
	}
	if len(narrow.Messages) >= len(wide.Messages) {
		t.Errorf("上限应减少回放量: capped=%d uncapped=%d", len(narrow.Messages), len(wide.Messages))
	}
	if narrow.HistoryBudgetTokens != capped.MaxHistoryTokens {
		t.Errorf("历史预算应等于上限(%d),实际 %d", capped.MaxHistoryTokens, narrow.HistoryBudgetTokens)
	}

	// 工具循环走 FitMessages:它必须还能用满窗口(不被历史上限牵着走)
	messages := make([]llmproto.ChatMessage, 0, len(wide.Messages))
	messages = append(messages, wide.Messages...)
	fittedCapped := FitMessages(dataobject.TLlm{ContextWindow: uint32(largeWindow)}, messages, capped)
	fittedUncapped := FitMessages(dataobject.TLlm{ContextWindow: uint32(largeWindow)}, messages, uncapped)
	if len(fittedCapped) != len(fittedUncapped) {
		t.Errorf("历史回放上限不得影响工具循环的窗口贴合: capped=%d uncapped=%d",
			len(fittedCapped), len(fittedUncapped))
	}
}

// TestHistoryCapNeverExhaustsBudget 上限不会把预算压到 0:BudgetExhausted 只由"窗口被预留吃光"造成。
func TestHistoryCapNeverExhaustsBudget(t *testing.T) {
	records := capRecords(20)
	policy := ContextPolicy{ContextWindow: 100000, OutputReserve: 1000, SystemPromptReserveTokens: 20000, MaxHistoryTokens: 1}

	result := AssembleContext("SYS", records, "问题", policy)
	if result.BudgetExhausted {
		t.Fatalf("上限很小也不应判为预算耗尽: %s", compactResult(result))
	}
	if result.HistoryBudgetTokens != 1 {
		t.Errorf("预算应收窄到上限 1,实际 %d", result.HistoryBudgetTokens)
	}
	// 一条都放不下 → 不带历史(但仍保留 system + 本轮问题)
	if result.DroppedRecordCount == 0 {
		t.Errorf("预算为 1 token 时应裁掉全部历史: %s", compactResult(result))
	}
}

// TestBudgetDecompositionIsObservable 预算分解必须能直接读到(排查"历史为什么只剩这么多")。
func TestBudgetDecompositionIsObservable(t *testing.T) {
	records := capRecords(30)
	policy := ContextPolicy{
		ContextWindow: 50000, OutputReserve: 1000,
		SystemPromptReserveTokens: 9000, ToolsReserveTokens: 3000, MaxHistoryTokens: 10000,
	}

	result := AssembleContext(strings.Repeat("系", 100), records, "问题", policy)

	if got, want := result.SystemReserveTokens, policy.SystemPromptReserveTokens; got != want {
		t.Errorf("系统预留应为 %d,实际 %d", want, got)
	}
	if result.ToolsReserveTokens != policy.ToolsReserveTokens {
		t.Errorf("工具预留应为 %d,实际 %d", policy.ToolsReserveTokens, result.ToolsReserveTokens)
	}
	if result.HistoryCapTokens != policy.MaxHistoryTokens {
		t.Errorf("上限应为 %d,实际 %d", policy.MaxHistoryTokens, result.HistoryCapTokens)
	}
	if result.HistoryBudgetTokens != policy.MaxHistoryTokens {
		t.Errorf("历史预算应被上限收窄到 %d,实际 %d", policy.MaxHistoryTokens, result.HistoryBudgetTokens)
	}
	if result.KeptHistoryTokens <= 0 || result.KeptHistoryTokens > result.HistoryBudgetTokens {
		t.Errorf("实际回放量应落在预算内且为正: %d (预算 %d)", result.KeptHistoryTokens, result.HistoryBudgetTokens)
	}
}

// TestGetContextPolicyHistoryTokens 策略推导:0 → 默认;正数 → 原值;负数 → 不限制。
func TestGetContextPolicyHistoryTokens(t *testing.T) {
	if got := GetContextPolicy(dataobject.TAgent{}, 100000).MaxHistoryTokens; got != 0 {
		// GetContextPolicy 原样透传;默认值在 normalizePolicy(装配时)兜
		t.Errorf("未配置时应透传 0(由装配层兜默认),实际 %d", got)
	}

	explicit := GetContextPolicy(dataobject.TAgent{MaxHistoryTokens: 12345}, 100000)
	if explicit.MaxHistoryTokens != 12345 {
		t.Errorf("显式配置应原样生效: %d", explicit.MaxHistoryTokens)
	}
	unlimited := GetContextPolicy(dataobject.TAgent{MaxHistoryTokens: -1}, 100000)
	if unlimited.MaxHistoryTokens != -1 {
		t.Errorf("负数应表示不限制: %d", unlimited.MaxHistoryTokens)
	}

	// 装配时:0 → DefaultMaxHistoryTokens
	result := AssembleContext("SYS", capRecords(5), "问题", ContextPolicy{ContextWindow: 500000, OutputReserve: 1000})
	if result.HistoryCapTokens != hCommon.DefaultMaxHistoryTokens {
		t.Errorf("未配置上限时应用默认 %d,实际 %d", hCommon.DefaultMaxHistoryTokens, result.HistoryCapTokens)
	}

	// 且默认上限确实会让"长历史"开始被裁(这正是它的用途:让压缩有可控的触发点)
	long := AssembleContext("SYS", capRecords(3000), "问题", ContextPolicy{ContextWindow: 1000000, OutputReserve: 1000})
	if long.DroppedRecordCount == 0 {
		t.Errorf("默认上限(%d token)下 3000 条记录应被裁: %s", hCommon.DefaultMaxHistoryTokens, compactResult(long))
	}
}

// TestEstimateToolsTokens 工具 schema 必须计入预算(空工具为 0)。
func TestEstimateToolsTokens(t *testing.T) {
	if got := EstimateToolsTokens(nil); got != 0 {
		t.Errorf("没有工具时应为 0,实际 %d", got)
	}

	tool := llmproto.Tool{
		Type: "function",
		Function: llmproto.ToolFunction{
			Name:        "extract_metrics",
			Description: "从尽调报告中抽取指标",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "报告文件路径"},
				},
			},
		},
	}
	one := EstimateToolsTokens([]llmproto.Tool{tool})
	if one <= 0 {
		t.Fatal("一个工具也应占用正数 token")
	}
	many := EstimateToolsTokens([]llmproto.Tool{tool, tool, tool, tool})
	if many <= one {
		t.Errorf("工具越多占用越大: 1 个 %d, 4 个 %d", one, many)
	}
}

// TestPolicyWarnings 配置异味必须被指出来(而不是静默失效)。
func TestPolicyWarnings(t *testing.T) {
	// ① 预留吃光窗口 → 历史永不回放 + 压缩被禁用
	broken := ContextPolicy{ContextWindow: 131072, OutputReserve: 4096, SystemPromptReserveTokens: 130000}
	warnings := PolicyWarnings(broken, 1600)
	if len(warnings) == 0 || !containsAny(warnings, "never be replayed") {
		t.Errorf("预留吃光窗口应告警,实际 %v", warnings)
	}

	// ② 预留远大于实测系统提示 → 疑似把系统预留当历史旋钮
	misused := ContextPolicy{ContextWindow: 1048576, OutputReserve: 4096, SystemPromptReserveTokens: 104857}
	warnings = PolicyWarnings(misused, 1592)
	if len(warnings) == 0 || !containsAny(warnings, "MaxHistoryTokens") {
		t.Errorf("预留是实测的 66 倍应提示改用 MaxHistoryTokens,实际 %v", warnings)
	}

	// ③ 健康配置:不打扰
	healthy := ContextPolicy{ContextWindow: 131072, OutputReserve: 4096, SystemPromptReserveTokens: 8192, MaxHistoryTokens: 65536}
	if warnings := PolicyWarnings(healthy, 1592); len(warnings) != 0 {
		t.Errorf("健康配置不应告警: %v", warnings)
	}

	// 未配置窗口:不做预算推断,也就不告警
	if warnings := PolicyWarnings(ContextPolicy{}, 1592); len(warnings) != 0 {
		t.Errorf("未配置窗口时不应告警: %v", warnings)
	}
}

func containsAny(items []string, sub string) bool {
	for _, item := range items {
		if strings.Contains(item, sub) {
			return true
		}
	}
	return false
}
