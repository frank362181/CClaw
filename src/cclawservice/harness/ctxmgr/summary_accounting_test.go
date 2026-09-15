/**
* 摘要体积的**唯一口径** —— F8
*
* 摘要体积同时被两处使用,而它们必须给出同一个结论:
*  1. 触发判定:`AssembleResult.Summary`(折进 system 的那段文本);
*  2. 动作门:`SummaryOverBudget(records)`(文件里的记录)。
* 两处不同源就会在阈值附近出现"判定要合并、动作说没超"(或反过来)的矛盾决定。
**/

package ctxmgr

import (
	"strings"
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
)

// summaryFixture 构造两条互不覆盖的摘要(序号 9 / 18)+ 若干普通记录(测试辅助)。
func summaryFixture(summaryRunes int) []hCommon.MemoryRecord {
	return []hCommon.MemoryRecord{
		{Index: 1, RecordType: hCommon.HistoryRecordTypeUser, Content: "第一轮问题"},
		{Index: 2, RecordType: hCommon.HistoryRecordTypeAssistant, Content: "第一轮回答"},
		{
			Index: 9, RecordType: hCommon.HistoryRecordTypeSummary,
			Content: strings.Repeat("一", summaryRunes), CoversFrom: 1, CoversTo: 8,
		},
		{Index: 10, RecordType: hCommon.HistoryRecordTypeUser, Content: "第二轮问题"},
		{
			Index: 18, RecordType: hCommon.HistoryRecordTypeSummary,
			Content: strings.Repeat("二", summaryRunes), CoversFrom: 10, CoversTo: 17,
		},
	}
}

// TestSummaryRuneAccountingHasOneSource 记录侧与注入文本侧必须是同一个数、同一个结论。
func TestSummaryRuneAccountingHasOneSource(t *testing.T) {
	// 两条摘要各 (上限/2 - 1) rune:加上它们之间的空行(2 rune)恰好等于上限 —— 卡在临界点上
	records := summaryFixture(hCommon.MaxSummaryTotalRunes/2 - 1)
	asm := AssembleContext("系统提示", records, "本轮问题", ContextPolicy{ContextWindow: 1 << 20})

	injected := hCommon.SummaryTextOf(records)
	if asm.Summary != injected {
		t.Fatalf("装配注入的摘要文本必须来自共享渲染器:\n注入=%q\n渲染=%q", asm.Summary, injected)
	}
	if !strings.Contains(asm.Messages[0].Content, asm.Summary) {
		t.Error("摘要必须真的折进 system 消息(否则\"注入体积\"无从谈起)")
	}

	if got := hCommon.SummariesRunes(records); got != hCommon.TextRunes(injected) {
		t.Errorf("记录侧体积(%d)必须等于注入文本长度(%d)", got, hCommon.TextRunes(injected))
	}
	if got := hCommon.TextRunes(injected); got != hCommon.MaxSummaryTotalRunes {
		t.Fatalf("用例前提:注入体积应恰好等于上限 %d,实际 %d", hCommon.MaxSummaryTotalRunes, got)
	}

	// 临界:恰好等于上限 —— 两个入口都必须判"未超"
	if SummaryOverBudget(records) {
		t.Error("动作门:恰好等于上限不应判为超预算")
	}
	if SummaryTextOverBudget(asm.Summary) {
		t.Error("触发判定:恰好等于上限不应判为超预算")
	}

	// 超 1 rune —— 两个入口都必须判"超",且结论一致
	over := summaryFixture(hCommon.MaxSummaryTotalRunes / 2)
	overAsm := AssembleContext("系统提示", over, "本轮问题", ContextPolicy{ContextWindow: 1 << 20})
	if !SummaryOverBudget(over) {
		t.Error("动作门:超过上限应判为超预算")
	}
	if !SummaryTextOverBudget(overAsm.Summary) {
		t.Error("触发判定:超过上限应判为超预算")
	}
	if SummaryOverBudget(over) != SummaryTextOverBudget(overAsm.Summary) {
		t.Error("两个入口必须给出同一结论")
	}
}

// TestSummaryRunesCountsTrimmedBodyOnly 体积只算**生效摘要**、且按 TrimSpace 后的正文计 ——
// 空正文不注入,自然也不该占体积;被取代的摘要不再注入,同样不该占。
func TestSummaryRunesCountsTrimmedBodyOnly(t *testing.T) {
	active := hCommon.MemoryRecord{
		Index: 9, RecordType: hCommon.HistoryRecordTypeSummary,
		Content: "  八百个字的摘要  ", CoversFrom: 1, CoversTo: 8,
	}
	empty := hCommon.MemoryRecord{
		Index: 18, RecordType: hCommon.HistoryRecordTypeSummary,
		Content: "   ", CoversFrom: 10, CoversTo: 17,
	}

	records := []hCommon.MemoryRecord{active, empty}
	if got, want := hCommon.SummariesRunes(records), hCommon.TextRunes("八百个字的摘要"); got != want {
		t.Errorf("体积应只算 TrimSpace 后的正文: got=%d want=%d", got, want)
	}

	// 被取代的摘要(更晚的摘要 covers 覆盖了它自己的序号)不再注入,也不计体积
	merged := hCommon.MemoryRecord{
		Index: 30, RecordType: hCommon.HistoryRecordTypeSummary,
		Content: "合并后的摘要", CoversFrom: 1, CoversTo: 18,
	}
	withMerged := append(records, merged)
	if got := hCommon.SummariesRunes(withMerged); got != hCommon.TextRunes("合并后的摘要") {
		t.Errorf("被取代的摘要不得计入体积: got=%d", got)
	}
}
