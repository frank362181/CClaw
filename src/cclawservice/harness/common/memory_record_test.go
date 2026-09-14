package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"my.favorite/cclawservice/harness/mstorage"
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

	if records[0].Index != 1 || records[0].RecordType != HistoryRecordUser {
		t.Errorf("第 1 条应为 user 且序号为 1: %+v", records[0])
	}
	if records[0].Content != "杭州明天天气怎么样？" {
		t.Errorf("user 正文不正确: %q", records[0].Content)
	}
	if records[0].TimeText != "2026-09-13 01:00:00" {
		t.Errorf("时间文本解析不正确: %q", records[0].TimeText)
	}

	if records[1].RecordType != HistoryRecordTool || records[1].Title != "get_weather" {
		t.Errorf("第 2 条应为 tool:get_weather: %+v", records[1])
	}
	if !strings.Contains(records[1].Content, `"weather":"晴"`) {
		t.Errorf("工具输出正文丢失: %q", records[1].Content)
	}

	if records[2].RecordType != HistoryRecordAssistant || records[2].IsInterrupted() {
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

// TestHistoryFileReadAppendRoundTrip 写一条读一条:序号、类型、标题、正文、时间全部一致。
func TestHistoryFileReadAppendRoundTrip(t *testing.T) {
	writer, sessionId := mstorage.newHistoryTestWriter(t)

	fileName, err := writer.Ensure(sessionId, nil, []string{"session_id: " + sessionId})
	if err != nil {
		t.Fatalf("Ensure 失败: %v", err)
	}
	if _, err = writer.Append(sessionId, fileName, HistoryRecordUser, "", "杭州明天天气怎么样？", "", 1); err != nil {
		t.Fatalf("Append user 失败: %v", err)
	}
	if _, err = writer.Append(sessionId, fileName, HistoryRecordTool, "get_weather", `{"city":"杭州"}`, "", 2); err != nil {
		t.Fatalf("Append tool 失败: %v", err)
	}
	if _, err = writer.Append(sessionId, fileName, HistoryRecordAssistant, "", "明天晴。", "", 3); err != nil {
		t.Fatalf("Append assistant 失败: %v", err)
	}

	records, err := writer.Read(sessionId, fileName)
	if err != nil {
		t.Fatalf("Read 失败: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("应读回 3 条记录,实际 %d: %+v", len(records), records)
	}
	for i, want := range []struct {
		index       int
		kind, title string
	}{
		{1, HistoryRecordUser, ""},
		{2, HistoryRecordTool, "get_weather"},
		{3, HistoryRecordAssistant, ""},
	} {
		got := records[i]
		if got.Index != want.index || got.RecordType != want.kind || got.Title != want.title {
			t.Errorf("第 %d 条不匹配: got=%+v want=%+v", i, got, want)
		}
	}
	if records[2].Content != "明天晴。" {
		t.Errorf("助手正文读回不正确: %q", records[2].Content)
	}
	if records[0].TimeText == "" {
		t.Errorf("时间文本应被写入并读回")
	}
}

// TestHistoryFileReadIncremental 追加后再次读回:只增量解析新增字节,记录累积且不重复。
func TestHistoryFileReadIncremental(t *testing.T) {
	writer, sessionId := mstorage.newHistoryTestWriter(t)

	fileName, _ := writer.Ensure(sessionId, nil, nil)
	if _, err := writer.Append(sessionId, fileName, HistoryRecordUser, "", "第一问", "", 1); err != nil {
		t.Fatalf("Append 失败: %v", err)
	}
	first, err := writer.Read(sessionId, fileName)
	if err != nil || len(first) != 1 {
		t.Fatalf("首次读回应为 1 条: %v, %+v", err, first)
	}

	path, _ := writer.resolvePath(sessionId, fileName)
	writer.mu.Lock()
	offsetBefore := writer.records[path].parsedOffset
	writer.mu.Unlock()
	if offsetBefore <= 0 {
		t.Fatalf("首次解析后 parsedOffset 应大于 0")
	}

	if _, err = writer.Append(sessionId, fileName, HistoryRecordAssistant, "", "第一答", "", 2); err != nil {
		t.Fatalf("Append 失败: %v", err)
	}
	second, err := writer.Read(sessionId, fileName)
	if err != nil {
		t.Fatalf("增量读回失败: %v", err)
	}
	if len(second) != 2 {
		t.Fatalf("增量读回应累积到 2 条,实际 %d: %+v", len(second), second)
	}
	if second[0].Content != "第一问" || second[1].Content != "第一答" {
		t.Errorf("增量合并顺序不正确: %+v", second)
	}

	// 未变化时应命中缓存(零 IO):parsedOffset 不再前移
	writer.mu.Lock()
	offsetAfter := writer.records[path].parsedOffset
	writer.mu.Unlock()
	if offsetAfter <= offsetBefore {
		t.Errorf("增量解析应推进 parsedOffset: before=%d after=%d", offsetBefore, offsetAfter)
	}
	if _, err = writer.Read(sessionId, fileName); err != nil {
		t.Fatalf("重复读回失败: %v", err)
	}
	writer.mu.Lock()
	offsetRepeated := writer.records[path].parsedOffset
	writer.mu.Unlock()
	if offsetRepeated != offsetAfter {
		t.Errorf("文件未变化时不应重复解析: %d -> %d", offsetAfter, offsetRepeated)
	}
}

// TestHistoryFileReadDetectsExternalRewrite 文件被外部改写(非追加)时,
// 头部指纹不一致,必须回退全量解析,不返回陈旧记录。
func TestHistoryFileReadDetectsExternalRewrite(t *testing.T) {
	writer, sessionId := mstorage.newHistoryTestWriter(t)

	fileName, _ := writer.Ensure(sessionId, nil, nil)
	if _, err := writer.Append(sessionId, fileName, HistoryRecordUser, "", "原始内容", "", 1); err != nil {
		t.Fatalf("Append 失败: %v", err)
	}
	if _, err := writer.Read(sessionId, fileName); err != nil {
		t.Fatalf("读回失败: %v", err)
	}

	// 模拟外部改写:重写整个文件(头部也随之变化),且总字节数变大
	path, _ := writer.resolvePath(sessionId, fileName)
	rewritten := "# NewSession " + sessionId + "\n\n- rewritten: yes\n\n---\n\n" +
		"## [1] user | 2026-09-13 05:00:00\n\n改写后的第一问\n\n" +
		"## [2] assistant | 2026-09-13 05:00:01\n\n改写后的第一答\n\n"
	if err := os.WriteFile(path, []byte(rewritten), 0o644); err != nil {
		t.Fatalf("重写历史文件失败: %v", err)
	}

	records, err := writer.Read(sessionId, fileName)
	if err != nil {
		t.Fatalf("改写后读回失败: %v", err)
	}
	if len(records) != 2 || records[0].Content != "改写后的第一问" {
		t.Fatalf("外部改写后应全量重解析: %+v", records)
	}
}

// TestHistoryFileReadMissingFile 会话尚未写入历史时,读回应返回空且无错误。
func TestHistoryFileReadMissingFile(t *testing.T) {
	writer, sessionId := mstorage.newHistoryTestWriter(t)

	records, err := writer.Read(sessionId, "memory_20260913000000.md")
	if err != nil {
		t.Fatalf("文件不存在不应报错: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("文件不存在应返回空记录: %+v", records)
	}
}

// TestHistoryFileReadRejectsIllegalName 读路径同样必须拒绝非法文件名与非法会话标识(防目录穿越)。
func TestHistoryFileReadRejectsIllegalName(t *testing.T) {
	writer, sessionId := mstorage.newHistoryTestWriter(t)

	if _, err := writer.Read(sessionId, "../../etc/passwd"); err == nil {
		t.Errorf("非法文件名应被拒绝")
	}
	if _, err := writer.Read("../evil", "memory_20260913000000.md"); err == nil {
		t.Errorf("非法会话标识应被拒绝")
	}
}

// TestHistoryFileReadDoesNotCreateDir 读路径不应产生副作用(不建目录)。
func TestHistoryFileReadDoesNotCreateDir(t *testing.T) {
	writer, sessionId := mstorage.newHistoryTestWriter(t)

	if _, err := writer.Read(sessionId, "memory_20260913000000.md"); err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	dir, _ := writer.MemoryDir(sessionId)
	if _, err := os.Stat(filepath.Join(dir)); err == nil {
		t.Errorf("读路径不应创建目录: %s", dir)
	}
}

// TestHistoryFileReadIncrementalAcrossProcess 模拟进程重启(新写入器,无缓存):
// 新实例必须能从文件中恢复出全部历史记录。
func TestHistoryFileReadIncrementalAcrossProcess(t *testing.T) {
	writer, sessionId := mstorage.newHistoryTestWriter(t)

	fileName, _ := writer.Ensure(sessionId, nil, nil)
	if _, err := writer.Append(sessionId, fileName, HistoryRecordUser, "", "重启前的问", "", 1); err != nil {
		t.Fatalf("Append 失败: %v", err)
	}

	restarted := &mstorage.MemoryFileRWriter{nextIndex: make(map[string]int)}
	records, err := restarted.Read(sessionId, fileName)
	if err != nil {
		t.Fatalf("重启后读回失败: %v", err)
	}
	if len(records) != 1 || records[0].Content != "重启前的问" {
		t.Fatalf("重启后应恢复全部历史: %+v", records)
	}

	// 重启后继续追加:序号必须延续(只增不减)
	index, err := restarted.Append(sessionId, fileName, HistoryRecordAssistant, "", "重启后的答", "", 0)
	if err != nil {
		t.Fatalf("重启后 Append 失败: %v", err)
	}
	if index != 2 {
		t.Errorf("重启后序号应延续为 2,实际 %d", index)
	}
}
