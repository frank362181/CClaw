/**
* 蒸馏压力(内容维度)的取值契约
*
* 这个数决定"要不要提前蒸",算错了会静默漏蒸(尾部长期进不了长期记忆)或每轮白烧调用。
**/

package mstorage

import (
	"context"
	"os"
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
)

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
	if chars != hCommon.RecordsRunes(records[:6]) {
		t.Errorf("正文长度应为 %d,实际 %d", hCommon.RecordsRunes(records[:6]), chars)
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
