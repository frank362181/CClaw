/**
* 蒸馏压力(内容维度)的取值契约
*
* 这个数决定"要不要提前蒸",算错了会静默漏蒸(尾部长期进不了长期记忆)或每轮白烧调用。
**/

package mstorage

import (
	"context"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
)

// renderedLinesRunes 一批记录按**送批口径**(送审行)计算的体积(测试辅助)。
func renderedLinesRunes(records []hCommon.MemoryRecord) int {
	total := 0
	for _, rec := range records {
		total += utf8.RuneCountInString(compactionPromptLine(rec))
	}
	return total
}

// TestPromptLineRunesMatchesRendered 回归(F12):"数出来的体积"必须等于"渲染出来的体积"。
//
// 两者服务于同一件事(蒸馏压力判定 vs 选批预算):任何一边改了公式而另一边没跟上,
// 就会出现"压力说攒够一轮了"而实际批次只装了零头(长工具记录),或反过来(多条短记录)。
// 公式与渲染是同一来源,这条用例把它们钉在一起。
func TestPromptLineRunesMatchesRendered(t *testing.T) {
	cases := []hCommon.MemoryRecord{
		record(1, hCommon.HistoryRecordTypeUser, "", "短问题"),
		record(12, hCommon.HistoryRecordTypeAssistant, "", strings.Repeat("长正文", 1500)), // 超 MemoryExcerptRunes → 带截断标记
		record(3, hCommon.HistoryRecordTypeTool, "get_weather", "工具输出"),
		record(4, hCommon.HistoryRecordTypeTool, "", "无标题工具"),
		{Index: 5, RecordType: hCommon.HistoryRecordTypeAssistant, Title: hCommon.HistoryRecordTypeInterrupted, Content: "被中断的回复"},
		record(6, hCommon.HistoryRecordTypeUser, "", "  两侧有空白  "),
		{Index: 7, RecordType: hCommon.HistoryRecordTypeSummary, Content: "covers: 1-6\n\n摘要"},
		{Index: 12345, RecordType: hCommon.HistoryRecordTypeAssistant, Content: ""},
	}

	for _, rec := range cases {
		if got, want := promptLineRunes(rec), utf8.RuneCountInString(compactionPromptLine(rec)); got != want {
			t.Errorf("index=%d: 数出来=%d,渲染出来=%d", rec.Index, got, want)
		}
	}
}

// TestDistillPressureUsesRenderedVolume 回归(F12)的行为面:一条超长工具记录**不再**独自把压力顶过门槛。
//
// 旧口径按原文计:12 万 rune 的工具输出 = 12 万 rune 压力(≥ DistillMaxInputChars),
// 于是每隔 3 轮就触发一次蒸馏,而实际送批只有 2000 rune 的截断片段 —— 而且工具记录不构成长期信号,
// 必然空跑。
func TestDistillPressureUsesRenderedVolume(t *testing.T) {
	agent := newDistillAgent(t, "distill_pressure_rendered")
	svc := &MemoryService{enabled: true}

	huge := record(1, hCommon.HistoryRecordTypeTool, "big_tool", strings.Repeat("很长的工具输出", 20000)) // ≈12 万 rune
	pending, chars := svc.DistillPressure(dataobject.TSession{SessionId: "s1"}, agent, []hCommon.MemoryRecord{huge})

	if pending != 1 {
		t.Fatalf("应统计 1 条待蒸馏记录,实际 %d", pending)
	}
	if chars >= hCommon.DistillMaxInputChars {
		t.Errorf("压力应按送审体积计(截断片段 %d rune 量级),实际 %d —— 已把一轮门槛顶过", hCommon.MemoryExcerptRunes, chars)
	}
	if chars < hCommon.MemoryExcerptRunes {
		t.Errorf("截断后的正文应有 %d rune 量级,实际 %d", hCommon.MemoryExcerptRunes, chars)
	}
}

// TestDistillPressureCountsAfterWatermark 只统计水位之后的普通记录。
func TestDistillPressureCountsAfterWatermark(t *testing.T) {
	agent := newDistillAgent(t, "distill_pressure")
	svc := &MemoryService{enabled: true}
	session := dataobject.TSession{SessionId: "s1"}

	records := distillRecords() // 6 条普通记录
	// 一条摘要记录:它自己不算待蒸馏内容;覆盖区间刻意不落在 1-6 上,
	// 否则那 6 条会被判为"已由摘要代表",蒸馏输入就空了(那是另一条规则,另有用例)
	records = append(records, hCommon.MemoryRecord{
		Index: 7, RecordType: hCommon.HistoryRecordTypeSummary, Content: "covers: 100-105\n\n摘要", CoversFrom: 100, CoversTo: 105,
	})

	// 还没有 MEMORY.md → 水位 0 → 全部普通记录都算待蒸馏(摘要记录不算)
	pending, chars := svc.DistillPressure(session, agent, records)
	if pending != 6 {
		t.Errorf("水位为 0 时应统计全部普通记录(6 条),实际 %d", pending)
	}
	// 体积口径 = **送批口径**(逐条送审行的 rune 数),不是原文长度 —— 见 TestPromptLineRunesMatchesRendered
	if want := renderedLinesRunes(records[:6]); chars != want {
		t.Errorf("体积应按送审行计:%d,实际 %d", want, chars)
	}

	// 蒸馏一次 → 水位推进到 6 → 无待蒸馏
	if _, err := svc.Distill(context.Background(), session, records, agent,
		fakeSummarizer("- 以后所有报告统一使用中文", nil)); err != nil {
		t.Fatalf("Distill 失败: %v", err)
	}
	path, _ := memoryDocPath(agent)
	if content, _ := os.ReadFile(path); len(content) == 0 {
		t.Fatalf("首轮蒸馏应写出 MEMORY.md(水位是后续判定的依据)")
	}
	if pending, chars = svc.DistillPressure(session, agent, records); pending != 0 || chars != 0 {
		t.Errorf("水位已覆盖全部记录时应无压力,实际 pending=%d chars=%d", pending, chars)
	}

	// 新增 2 条 → 只统计水位之后的
	more := append(records, record(8, hCommon.HistoryRecordTypeUser, "", "以后统一口径"),
		record(9, hCommon.HistoryRecordTypeAssistant, "", "好的"))
	if pending, chars = svc.DistillPressure(session, agent, more); pending != 2 || chars <= 0 {
		t.Errorf("应只统计水位之后的 2 条,实际 pending=%d chars=%d", pending, chars)
	}
}

// TestDistillPressureStopsAtOneBatch 攒够一轮就早退:待蒸馏量远超送入预算时不做全量扫描
// (这个判断在请求路径上,必须便宜)。
func TestDistillPressureStopsAtOneBatch(t *testing.T) {
	agent := newDistillAgent(t, "distill_pressure_scan")
	svc := &MemoryService{enabled: true}

	const total = 50
	var records []hCommon.MemoryRecord
	for i := 1; i <= total; i++ {
		records = append(records, record(i, hCommon.HistoryRecordTypeUser, "", strings1000()))
	}

	pending, chars := svc.DistillPressure(dataobject.TSession{SessionId: "s1"}, agent, records)
	if chars < hCommon.DistillMaxInputChars {
		t.Fatalf("早退阈值应至少到一个批次:%d", chars)
	}
	if pending >= total {
		t.Errorf("应在攒够一个批次时停止扫描: pending=%d total=%d", pending, total)
	}
}

// strings1000 造一段约 1000 rune 的正文。
func strings1000() string {
	body := make([]rune, 0, 1000)
	for len(body) < 1000 {
		body = append(body, '内', '容')
	}
	return string(body)
}
