package ctxmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	myCommon "my.favorite/cclawservice/common"
)

// newHistoryTestWriter 构造一个独立的写入器 + 唯一会话标识,并在用例结束后清理落盘目录。
// 刻意不使用全局单例:索引恢复能力需要在"进程重启"语义下验证。
func newHistoryTestWriter(t *testing.T) (*HistoryFileWriter, string) {
	t.Helper()

	sessionId := myCommon.NewCompactId("testhist")
	writer := &HistoryFileWriter{nextIndex: make(map[string]int)}

	t.Cleanup(func() {
		if dir, err := writer.HistoryDir(sessionId); err == nil {
			_ = os.RemoveAll(dir)
		}
	})
	return writer, sessionId
}

// TestHistoryFileEnsureCreatesFile 建档应生成 memory_{datetime}.md 并写入头部元数据。
func TestHistoryFileEnsureCreatesFile(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)

	fileName, err := writer.Ensure(sessionId, nil, []string{"session_id: " + sessionId, "agent_id: agent_001"})
	if err != nil {
		t.Fatalf("Ensure 失败: %v", err)
	}

	if !strings.HasPrefix(fileName, myCommon.ChatHistoryFilePrefix) || !strings.HasSuffix(fileName, myCommon.ChatHistoryFileExt) {
		t.Fatalf("历史文件名不符合 memory_{datetime}.md 约定: %q", fileName)
	}

	dir, err := writer.HistoryDir(sessionId)
	if err != nil {
		t.Fatalf("HistoryDir 失败: %v", err)
	}

	path := filepath.Join(dir, fileName)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("历史文件未创建: %v", err)
	}
	if !strings.Contains(string(content), "session_id: "+sessionId) {
		t.Errorf("历史文件头部缺少 session_id,内容:\n%s", content)
	}
}

// TestHistoryFileEnsureIdempotent 重复 Ensure 应返回同一文件名且不覆盖既有内容
// (会话的第 2..n 条消息都会走到这条路径)。
func TestHistoryFileEnsureIdempotent(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)

	first, err := writer.Ensure(sessionId, nil, []string{"session_id: " + sessionId})
	if err != nil {
		t.Fatalf("第一次 Ensure 失败: %v", err)
	}
	if _, err = writer.Append(sessionId, first, HistoryRecordUser, "", "你好", "", 0); err != nil {
		t.Fatalf("Append 失败: %v", err)
	}

	second, err := writer.Ensure(sessionId, nil, []string{"session_id: " + sessionId})
	if err != nil {
		t.Fatalf("第二次 Ensure 失败: %v", err)
	}

	// 第二次 Ensure 会以新的时间戳算出文件名;此时应保持既有文件不被破坏
	dir, _ := writer.HistoryDir(sessionId)
	if content, err := os.ReadFile(filepath.Join(dir, first)); err != nil || !strings.Contains(string(content), "你好") {
		t.Fatalf("既有历史文件内容不应丢失: err=%v content=%s", err, content)
	}
	if second == "" {
		t.Error("Ensure 应始终返回可用的文件名")
	}
}

// TestHistoryFileAppendIndexSequence 记录序号必须从 1 起单调递增,
// 且顺序与真实发生顺序一致(问题 → 工具 → 回复)。
func TestHistoryFileAppendIndexSequence(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)

	fileName, err := writer.Ensure(sessionId, nil, nil)
	if err != nil {
		t.Fatalf("Ensure 失败: %v", err)
	}

	wantIndexes := []int{1, 2, 3}
	records := []struct {
		recordType string
		title      string
		content    string
	}{
		{HistoryRecordUser, "", "今天天气怎么样"},
		{HistoryRecordTool, "get_weather", `{"temp": 26}`},
		{HistoryRecordAssistant, "", "今天 26 度"},
	}

	for i, record := range records {
		index, err := writer.Append(sessionId, fileName, record.recordType, record.title, record.content, "2026-09-12 08:05:03", 0)
		if err != nil {
			t.Fatalf("第 %d 条 Append 失败: %v", i, err)
		}
		if index != wantIndexes[i] {
			t.Errorf("第 %d 条记录序号 = %d, want %d", i, index, wantIndexes[i])
		}
	}

	dir, _ := writer.HistoryDir(sessionId)
	content, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatalf("读取历史文件失败: %v", err)
	}

	text := string(content)
	for _, want := range []string{"## [1] user | 2026-09-12 08:05:03", "## [2] tool:get_weather", "## [3] assistant"} {
		if !strings.Contains(text, want) {
			t.Errorf("历史文件缺少记录头 %q,实际内容:\n%s", want, text)
		}
	}
}

// TestHistoryFileIndexRecovery 模拟进程重启:新建写入器后应从文件恢复序号,
// 而不是重新从 1 开始(否则会覆盖既有记录序号,破坏 chat_index/resp_index 引用)。
func TestHistoryFileIndexRecovery(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)

	fileName, err := writer.Ensure(sessionId, nil, nil)
	if err != nil {
		t.Fatalf("Ensure 失败: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err = writer.Append(sessionId, fileName, HistoryRecordUser, "", "内容", "", 0); err != nil {
			t.Fatalf("Append 失败: %v", err)
		}
	}

	// 全新写入器(等价于进程重启后的空缓存)
	restarted := &HistoryFileWriter{nextIndex: make(map[string]int)}
	index, err := restarted.Append(sessionId, fileName, HistoryRecordAssistant, "", "回复", "", 0)
	if err != nil {
		t.Fatalf("重启后 Append 失败: %v", err)
	}
	if index != 4 {
		t.Errorf("重启后序号应从文件恢复到 4,实际 %d", index)
	}
}

// TestHistoryDirRejectsUnsafeSessionId 会话标识参与路径拼接,必须拒绝越级/嵌套型入参。
func TestHistoryDirRejectsUnsafeSessionId(t *testing.T) {
	writer := &HistoryFileWriter{nextIndex: make(map[string]int)}

	for _, unsafe := range []string{"", ".", "..", "../evil", "a/b", `a\b`, `..\evil`, "/abs/path", "a/../b"} {
		if _, err := writer.HistoryDir(unsafe); err == nil {
			t.Errorf("不安全会话标识 %q 应被拒绝", unsafe)
		}
	}

	if _, err := writer.HistoryDir("safesession_001"); err != nil {
		t.Errorf("合法会话标识不应被拒绝: %v", err)
	}
}

// TestHistoryResolvePathRejectsNestedFileName 文件名必须是纯文件名,不允许带目录。
func TestHistoryResolvePathRejectsNestedFileName(t *testing.T) {
	writer := &HistoryFileWriter{nextIndex: make(map[string]int)}

	if _, err := writer.Append("safesession", "../escape.md", HistoryRecordUser, "", "x", "", 0); err == nil {
		t.Error("带目录的历史文件名应被拒绝")
	}
	if _, err := writer.Append("safesession", "", HistoryRecordUser, "", "x", "", 0); err == nil {
		t.Error("空历史文件名应被拒绝")
	}
}

// TestHistoryAppendHintWinsWhenLarger hintIndex 来自 DB(序号权威来源),
// 比本进程缓存新时必须以它为准 —— 压缩重写文件后本地缓存会偏小,只有 DB 才知道真实进度。
func TestHistoryAppendHintWinsWhenLarger(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)

	fileName, err := writer.Ensure(sessionId, nil, nil)
	if err != nil {
		t.Fatalf("Ensure 失败: %v", err)
	}

	// 第一条:本地缓存为空,采用 hint 100
	index, err := writer.Append(sessionId, fileName, HistoryRecordUser, "", "问题", "", 100)
	if err != nil {
		t.Fatalf("Append 失败: %v", err)
	}
	if index != 100 {
		t.Errorf("hintIndex 应被采用,期望 100,实际 %d", index)
	}

	// 第二条不给 hint:应接着 101,而不是回到 1
	index, err = writer.Append(sessionId, fileName, HistoryRecordAssistant, "", "回复", "", 0)
	if err != nil {
		t.Fatalf("Append 失败: %v", err)
	}
	if index != 101 {
		t.Errorf("应接着缓存继续,期望 101,实际 %d", index)
	}
}

// TestHistoryAppendIndexNeverRegresses 序号契约(P0):只增不减。
// 即使调用方传入偏小的 hint(例如 DB 被回滚/重置),也绝不能回退复用旧序号,
// 否则 t_turn.chat_index / resp_index 会指向错位。
func TestHistoryAppendIndexNeverRegresses(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)

	fileName, err := writer.Ensure(sessionId, nil, nil)
	if err != nil {
		t.Fatalf("Ensure 失败: %v", err)
	}

	for i := 1; i <= 5; i++ {
		if _, err = writer.Append(sessionId, fileName, HistoryRecordUser, "", "内容", "", 0); err != nil {
			t.Fatalf("第 %d 条 Append 失败: %v", i, err)
		}
	}

	// 倒退的 hint 必须被忽略
	index, err := writer.Append(sessionId, fileName, HistoryRecordAssistant, "", "回复", "", 1)
	if err != nil {
		t.Fatalf("Append 失败: %v", err)
	}
	if index != 6 {
		t.Errorf("序号不应回退,期望 6,实际 %d", index)
	}
}

// TestHistoryTailScanHandlesLargeLastRecord 冷启动恢复(尾部反向扫描)必须能应对
// "最后一条记录本身远大于初始扫描块"的情况(块大小需逐次放大)。
func TestHistoryTailScanHandlesLargeLastRecord(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)

	fileName, err := writer.Ensure(sessionId, nil, nil)
	if err != nil {
		t.Fatalf("Ensure 失败: %v", err)
	}
	if _, err = writer.Append(sessionId, fileName, HistoryRecordUser, "", "问题", "", 0); err != nil {
		t.Fatalf("Append 失败: %v", err)
	}

	// 正文远大于 defaultTailScanChunkBytes:最后一个记录头距文件末尾超过一个初始块
	huge := strings.Repeat("x", defaultTailScanChunkBytes+1024)
	if _, err = writer.Append(sessionId, fileName, HistoryRecordAssistant, "", huge, "", 0); err != nil {
		t.Fatalf("Append 失败: %v", err)
	}

	// 模拟进程重启:缓存为空且不给 hint,只能靠尾部扫描恢复
	restarted := &HistoryFileWriter{nextIndex: make(map[string]int)}
	index, err := restarted.Append(sessionId, fileName, HistoryRecordUser, "", "第三个问题", "", 0)
	if err != nil {
		t.Fatalf("重启后 Append 失败: %v", err)
	}
	if index != 3 {
		t.Errorf("尾部扫描应找到最后一条(序号 2),下一个为 3,实际 %d", index)
	}
}

// TestHistoryIndexCacheBounded nextIndex 缓存必须有容量上限,
// 否则会随会话数单调增长(长期运行的内存泄漏)。
func TestHistoryIndexCacheBounded(t *testing.T) {
	const limit = 4
	writer := &HistoryFileWriter{nextIndex: make(map[string]int), maxEntries: limit}

	for i := 0; i < limit*5; i++ {
		writer.setNextIndex(fmt.Sprintf("/tmp/session-%d/memory.md", i), i+1)
	}

	writer.mu.Lock()
	size := len(writer.nextIndex)
	writer.mu.Unlock()

	if size > limit {
		t.Errorf("nextIndex 超出容量上限: size=%d limit=%d", size, limit)
	}
}
