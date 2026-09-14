/**
* 历史文件的读回用例(白盒):依赖 mstorage 内部结构(parsedOffset/records 缓存)与
* 包内测试辅助 newHistoryTestWriter,因此必须留在 mstorage 包内。
*
* 纯解析用例(ParseMemoryRecords)在 harness/common 的 memory_record_test.go。
**/

package mstorage

import (
	"os"
	"path/filepath"
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
)

// TestHistoryFileReadAppendRoundTrip 写一条读一条:序号、类型、标题、正文、时间全部一致。
func TestHistoryFileReadAppendRoundTrip(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)

	fileName, err := writer.Ensure(sessionId, nil, []string{"session_id: " + sessionId})
	if err != nil {
		t.Fatalf("Ensure 失败: %v", err)
	}
	if _, err = writer.Append(sessionId, fileName, hCommon.HistoryRecordTypeUser, "", "杭州明天天气怎么样？", "", 1); err != nil {
		t.Fatalf("Append user 失败: %v", err)
	}
	if _, err = writer.Append(sessionId, fileName, hCommon.HistoryRecordTypeTool, "get_weather", `{"city":"杭州"}`, "", 2); err != nil {
		t.Fatalf("Append tool 失败: %v", err)
	}
	if _, err = writer.Append(sessionId, fileName, hCommon.HistoryRecordTypeAssistant, "", "明天晴。", "", 3); err != nil {
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
		{1, hCommon.HistoryRecordTypeUser, ""},
		{2, hCommon.HistoryRecordTypeTool, "get_weather"},
		{3, hCommon.HistoryRecordTypeAssistant, ""},
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
	writer, sessionId := newHistoryTestWriter(t)

	fileName, _ := writer.Ensure(sessionId, nil, nil)
	if _, err := writer.Append(sessionId, fileName, hCommon.HistoryRecordTypeUser, "", "第一问", "", 1); err != nil {
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

	if _, err = writer.Append(sessionId, fileName, hCommon.HistoryRecordTypeAssistant, "", "第一答", "", 2); err != nil {
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
	writer, sessionId := newHistoryTestWriter(t)

	fileName, _ := writer.Ensure(sessionId, nil, nil)
	if _, err := writer.Append(sessionId, fileName, hCommon.HistoryRecordTypeUser, "", "原始内容", "", 1); err != nil {
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
	writer, sessionId := newHistoryTestWriter(t)

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
	writer, sessionId := newHistoryTestWriter(t)

	if _, err := writer.Read(sessionId, "../../etc/passwd"); err == nil {
		t.Errorf("非法文件名应被拒绝")
	}
	if _, err := writer.Read("../evil", "memory_20260913000000.md"); err == nil {
		t.Errorf("非法会话标识应被拒绝")
	}
}

// TestHistoryFileReadDoesNotCreateDir 读路径不应产生副作用(不建目录)。
func TestHistoryFileReadDoesNotCreateDir(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)

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
	writer, sessionId := newHistoryTestWriter(t)

	fileName, _ := writer.Ensure(sessionId, nil, nil)
	if _, err := writer.Append(sessionId, fileName, hCommon.HistoryRecordTypeUser, "", "重启前的问", "", 1); err != nil {
		t.Fatalf("Append 失败: %v", err)
	}

	restarted := &MemoryFileRWriter{nextIndex: make(map[string]int)}
	records, err := restarted.Read(sessionId, fileName)
	if err != nil {
		t.Fatalf("重启后读回失败: %v", err)
	}
	if len(records) != 1 || records[0].Content != "重启前的问" {
		t.Fatalf("重启后应恢复全部历史: %+v", records)
	}

	// 重启后继续追加:序号必须延续(只增不减)
	index, err := restarted.Append(sessionId, fileName, hCommon.HistoryRecordTypeAssistant, "", "重启后的答", "", 0)
	if err != nil {
		t.Fatalf("重启后 Append 失败: %v", err)
	}
	if index != 2 {
		t.Errorf("重启后序号应延续为 2,实际 %d", index)
	}
}
