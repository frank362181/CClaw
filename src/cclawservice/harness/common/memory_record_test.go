package common

import (
	"strings"
	"testing"
)

// TestParseMemoryRecordsBasic 解析:头部前导 + 元数据行 + 分隔线 + 三类记录。
func TestParseMemoryRecordsBasic(t *testing.T) {
	text := "# NewSession abc123\n\n- session_id: abc123\n- agent_id: agent_001\n\n---\n\n" +
		"## [1] user | 2026-09-13 01:00:00\n\n杭州明天天气怎么样？\n\n" +
		"## [2] tool:get_weather | 2026-09-13 01:00:05\n\n{\"city\":\"杭州\",\"weather\":\"晴\"}\n\n" +
		"## [3] assistant | 2026-09-13 01:00:09\n\n杭州明天晴，18~26℃。\n\n"

	records, consumed := ParseMemoryRecords(text)
	if len(records) != 3 {
		t.Fatalf("记录数应为 3,实际 %d", len(records))
	}
	if consumed != len(text) {
		t.Errorf("完整文本应全部消费: consumed=%d len=%d", consumed, len(text))
	}

	if records[0].Index != 1 || records[0].RecordType != HistoryRecordTypeUser {
		t.Errorf("第 1 条应为 user 且序号为 1: %+v", records[0])
	}
	if records[0].Content != "杭州明天天气怎么样？" {
		t.Errorf("user 正文不正确: %q", records[0].Content)
	}
	if records[0].TimeText != "2026-09-13 01:00:00" {
		t.Errorf("时间文本解析不正确: %q", records[0].TimeText)
	}

	if records[1].RecordType != HistoryRecordTypeTool || records[1].Title != "get_weather" {
		t.Errorf("第 2 条应为 tool:get_weather: %+v", records[1])
	}
	if !strings.Contains(records[1].Content, `"weather":"晴"`) {
		t.Errorf("工具输出正文丢失: %q", records[1].Content)
	}

	if records[2].RecordType != HistoryRecordTypeAssistant || records[2].IsInterrupted() {
		t.Errorf("第 3 条应为正常 assistant: %+v", records[2])
	}
}

// TestParseMemoryRecordsMultiLineAndBlankLines 正文中的空行必须原样保留。
func TestParseMemoryRecordsMultiLineAndBlankLines(t *testing.T) {
	text := "## [7] assistant | 2026-09-13 02:00:00\n\n第一段\n\n第二段\n\n"

	records, _ := ParseMemoryRecords(text)
	if len(records) != 1 {
		t.Fatalf("记录数应为 1,实际 %d", len(records))
	}
	if records[0].Content != "第一段\n\n第二段" {
		t.Errorf("多段正文解析不正确: %q", records[0].Content)
	}
}

// TestParseMemoryRecordsIncompleteTail 写入中断(末条无空行收尾)时:
// 不产出该条,且 consumed 停在它之前 —— 下次增量读取可重新解析。
func TestParseMemoryRecordsIncompleteTail(t *testing.T) {
	complete := "## [1] user | 2026-09-13 01:00:00\n\n你好\n\n"
	text := complete + "## [2] assistant | 2026-09-13 01:00:01\n\n写到一半"

	records, consumed := ParseMemoryRecords(text)
	if len(records) != 1 || records[0].Index != 1 {
		t.Fatalf("只应解析出第 1 条: %+v", records)
	}
	if consumed != len(complete) {
		t.Errorf("consumed 应停在完整记录之后: consumed=%d want=%d", consumed, len(complete))
	}
}

// TestParseMemoryRecordsSummaryCovers 摘要记录的 covers 标注必须解析出来,并从正文剔除。
func TestParseMemoryRecordsSummaryCovers(t *testing.T) {
	text := "## [100] summary | 2026-09-13 03:00:00\n\ncovers: 1-99\n\n" +
		"用户先咨询了杭州天气，助手给出了晴天的答复。\n\n"

	records, _ := ParseMemoryRecords(text)
	if len(records) != 1 {
		t.Fatalf("记录数应为 1,实际 %d", len(records))
	}
	rec := records[0]
	if !rec.IsSummary() {
		t.Fatalf("应识别为摘要记录: %+v", rec)
	}
	if rec.CoversFrom != 1 || rec.CoversTo != 99 {
		t.Errorf("covers 解析不正确: %d-%d", rec.CoversFrom, rec.CoversTo)
	}
	if strings.Contains(rec.Content, "covers:") {
		t.Errorf("正文中的 covers 行应被剔除: %q", rec.Content)
	}
	if !rec.Covers(50) || rec.Covers(100) || rec.Covers(0) {
		t.Errorf("Covers 区间判定不正确")
	}
}

// TestParseMemoryRecordsInterruptedMarker 中断标记必须可识别。
func TestParseMemoryRecordsInterruptedMarker(t *testing.T) {
	text := "## [5] assistant:interrupted | 2026-09-13 04:00:00\n\n回答到一半被用户中断\n\n"

	records, _ := ParseMemoryRecords(text)
	if len(records) != 1 || !records[0].IsInterrupted() {
		t.Fatalf("应识别为中断记录: %+v", records)
	}
	if records[0].Content != "回答到一半被用户中断" {
		t.Errorf("部分回复内容丢失: %q", records[0].Content)
	}
}

// TestParseMemoryRecordsIgnoresUnrelatedText 非记录行(含非行首的 "## [")不应被误判为记录头。
func TestParseMemoryRecordsIgnoresUnrelatedText(t *testing.T) {
	text := "## [1] user | 2026-09-13 01:00:00\n\n参考 ## [99] 不是记录头\n\n正文里出现 ## [xx] 也应忽略\n\n"

	records, _ := ParseMemoryRecords(text)
	if len(records) != 1 {
		t.Fatalf("只应解析出 1 条记录: %+v", records)
	}
	if !strings.Contains(records[0].Content, "## [99]") {
		t.Errorf("行内 ## [ 应保留在正文中: %q", records[0].Content)
	}
}
