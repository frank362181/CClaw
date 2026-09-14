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

// bulkBody 给测试记录补足正文。压缩有收益门槛(源体积不大于摘要下限时主动跳过),
// 因此压缩类用例的记录正文必须足够长。
func bulkBody() string { return strings.Repeat("细节说明", 60) } // 240 rune

// record 构造一条历史记录(测试辅助)
func record(index int, recordType, title, content string) hCommon.MemoryRecord {
	return hCommon.MemoryRecord{Index: index, RecordType: recordType, Title: title, Content: content}
}

// fakeSummarizer 返回固定摘要(测试辅助),同时统计调用次数。
func fakeSummarizer(text string, calls *int) Summarizer {
	return func(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
		if calls != nil {
			*calls++
		}
		return text, nil
	}
}

// compactRecords 构造 12 条普通记录(测试辅助):6 轮问答。
// 正文刻意写得足够长:压缩的价值来自"变小",源体积不大于摘要下限的区间会被收益门槛拦下。
func compactRecords() []hCommon.MemoryRecord {
	body := strings.Repeat("历史细节", 40) // 每条约 160 rune
	var records []hCommon.MemoryRecord
	index := 1
	for i := 0; i < 6; i++ {
		records = append(records,
			record(index, hCommon.HistoryRecordTypeUser, "", "问题"+string(rune('1'+i))+" "+body),
			record(index+1, hCommon.HistoryRecordTypeAssistant, "", "回答"+string(rune('1'+i))+" "+body),
		)
		index += 2
	}
	return records
}

// writeRecords 把记录写入历史文件(测试辅助),返回文件名。
func writeRecords(t *testing.T, writer *MemoryFileRWriter, sessionId string, records []hCommon.MemoryRecord) string {
	t.Helper()

	fileName, err := writer.Ensure(sessionId, nil, nil)
	if err != nil {
		t.Fatalf("Ensure 失败: %v", err)
	}
	for _, rec := range records {
		title := rec.Title
		if _, err = writer.Append(sessionId, fileName, rec.RecordType, title, rec.Content, "", 0); err != nil {
			t.Fatalf("Append 失败: %v", err)
		}
	}
	return fileName
}

// newCompactService 构造一个"启用但无 DB"的 MemoryService(压缩只用文件读写,不需要 DB)。
func newCompactService() *MemoryService {
	return &MemoryService{enabled: true}
}

// TestCompactWritesSummaryAppendedNotRenumbered 压缩必须"追加一条更大序号的摘要记录",
// 且不得重排/复用已有序号(P0 契约)。
func TestCompactWritesSummaryAppendedNotRenumbered(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)
	records := compactRecords()
	fileName := writeRecords(t, writer, sessionId, records)

	session := dataobject.TSession{SessionId: sessionId, ChatData: fileName}
	svc := newCompactService()

	// 用真实的读取器读回(与生产一致)
	readBack, err := GetMemoryFileWriter().Read(sessionId, fileName)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if len(readBack) != 12 {
		t.Fatalf("应写入 12 条记录,实际 %d", len(readBack))
	}

	result, err := svc.Compact(context.Background(), session, readBack, 1, 8, hCommon.CompactionModeDefault,
		fakeSummarizer("用户先问了 8 个问题,助手逐一回答。", nil))
	if err != nil {
		t.Fatalf("Compact 失败: %v", err)
	}
	if result.SummaryIndex != 13 {
		t.Fatalf("摘要应占用 13 号(原记录 1..12 之后),实际 %d", result.SummaryIndex)
	}
	if result.CoversFrom != 1 || result.CoversTo != 8 {
		t.Errorf("覆盖区间应为 1-8: %d-%d", result.CoversFrom, result.CoversTo)
	}

	// 读回:原记录一条不少(没有被删除/重排),末尾多出一条摘要
	after, err := GetMemoryFileWriter().Read(sessionId, fileName)
	if err != nil {
		t.Fatalf("压缩后读回失败: %v", err)
	}
	if len(after) != 13 {
		t.Fatalf("压缩后应有 13 条记录(12 原记录 + 1 摘要),实际 %d", len(after))
	}
	for i := 0; i < 12; i++ {
		if after[i].Index != i+1 || after[i].IsSummary() {
			t.Fatalf("原记录必须保持原序号且不被改写: %+v", after[i])
		}
	}
	summary := after[12]
	if !summary.IsSummary() || summary.CoversFrom != 1 || summary.CoversTo != 8 {
		t.Fatalf("末条应为覆盖 1-8 的摘要记录: %+v", summary)
	}
	if !strings.Contains(summary.Content, "逐一回答") {
		t.Errorf("摘要正文不正确: %q", summary.Content)
	}
	if strings.Contains(summary.Content, "covers:") {
		t.Errorf("covers 行应作为元数据被解析掉: %q", summary.Content)
	}
}

// TestCompactIdempotentSkipsCoveredRange 同一区间不得被重复压缩(幂等)。
func TestCompactIdempotentSkipsCoveredRange(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)
	fileName := writeRecords(t, writer, sessionId, compactRecords())
	session := dataobject.TSession{SessionId: sessionId, ChatData: fileName}
	svc := newCompactService()

	records, _ := GetMemoryFileWriter().Read(sessionId, fileName)
	if _, err := svc.Compact(context.Background(), session, records, 1, 8, hCommon.CompactionModeDefault,
		fakeSummarizer("第一次摘要。", nil)); err != nil {
		t.Fatalf("首次压缩失败: %v", err)
	}

	records, _ = GetMemoryFileWriter().Read(sessionId, fileName)
	second, err := svc.Compact(context.Background(), session, records, 1, 8, hCommon.CompactionModeDefault,
		fakeSummarizer("第二次摘要(不应写入)。", nil))
	if err != nil {
		t.Fatalf("二次压缩失败: %v", err)
	}
	if second.SummaryIndex != 0 {
		t.Errorf("已覆盖区间不应重复压缩: %+v", second)
	}

	after, _ := GetMemoryFileWriter().Read(sessionId, fileName)
	if len(after) != 13 {
		t.Errorf("重复压缩不得写入新记录: %d", len(after))
	}
}

// TestCompactSkipsTooSmallRange 区间太小(少于阈值)不触发压缩,避免浪费一次 LLM 调用。
func TestCompactSkipsTooSmallRange(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)
	fileName := writeRecords(t, writer, sessionId, compactRecords())
	session := dataobject.TSession{SessionId: sessionId, ChatData: fileName}
	svc := newCompactService()

	calls := 0
	records, _ := GetMemoryFileWriter().Read(sessionId, fileName)
	result, err := svc.Compact(context.Background(), session, records, 1, 2, hCommon.CompactionModeDefault,
		fakeSummarizer("摘要", &calls))
	if err != nil {
		t.Fatalf("Compact 失败: %v", err)
	}
	if result.SummaryIndex != 0 || calls != 0 {
		t.Errorf("过小的区间不应触发压缩: result=%+v calls=%d", result, calls)
	}
}

// TestCompactSafeguardDegradesWhenKeysLost 保全模式:摘要丢掉关键信息(路径/编号)时
// 必须降级(缩小覆盖区间),让较新的原文继续可回放。
func TestCompactSafeguardDegradesWhenKeysLost(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)
	records := []hCommon.MemoryRecord{
		record(1, hCommon.HistoryRecordTypeUser, "", "帮我看 /Volumes/Wisdom/MyIdea/CClaw/src/data/ddl_sqlite3.sql "+bulkBody()),
		record(2, hCommon.HistoryRecordTypeAssistant, "", "该文件共 123456 行,已阅读完成。"+bulkBody()),
		record(3, hCommon.HistoryRecordTypeUser, "", "再看 agentService/config/agentdefault.json "+bulkBody()),
		record(4, hCommon.HistoryRecordTypeAssistant, "", "已阅读完毕,包含 987654 个字段配置。"+bulkBody()),
		record(5, hCommon.HistoryRecordTypeUser, "", "继续分析 memory_service.go "+bulkBody()),
		record(6, hCommon.HistoryRecordTypeAssistant, "", "分析完成。"+bulkBody()),
	}
	fileName := writeRecords(t, writer, sessionId, records)
	session := dataobject.TSession{SessionId: sessionId, ChatData: fileName}
	svc := newCompactService()

	readBack, _ := GetMemoryFileWriter().Read(sessionId, fileName)
	result, err := svc.Compact(context.Background(), session, readBack, 1, 6, hCommon.CompactionModeSafeguard,
		fakeSummarizer("用户阅读了几个文件,内容已看。", nil)) // 刻意不含路径与编号
	if err != nil {
		t.Fatalf("Compact 失败: %v", err)
	}
	if !result.Degraded {
		t.Fatalf("关键信息丢失时必须降级: %+v", result)
	}
	if result.CoversTo >= 6 {
		t.Errorf("降级后覆盖区间应缩小,实际到 %d", result.CoversTo)
	}
	if result.CoversFrom != 1 || result.CoversTo < 1 {
		t.Errorf("覆盖起点仍应为 1 且终点有效: %d-%d", result.CoversFrom, result.CoversTo)
	}
}

// TestCompactSafeguardKeepsWhenKeysRetained 关键信息被保留时不降级。
func TestCompactSafeguardKeepsWhenKeysRetained(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)
	records := []hCommon.MemoryRecord{
		record(1, hCommon.HistoryRecordTypeUser, "", "查看 /Volumes/Wisdom/MyIdea/CClaw/src/data/ddl_sqlite3.sql "+bulkBody()),
		record(2, hCommon.HistoryRecordTypeAssistant, "", "该文件定义 123456 条约束。"+bulkBody()),
		record(3, hCommon.HistoryRecordTypeUser, "", "还有 config/agentdefault.json 呢"+bulkBody()),
		record(4, hCommon.HistoryRecordTypeAssistant, "", "含 987654 个配置项。"+bulkBody()),
	}
	fileName := writeRecords(t, writer, sessionId, records)
	session := dataobject.TSession{SessionId: sessionId, ChatData: fileName}
	svc := newCompactService()

	readBack, _ := GetMemoryFileWriter().Read(sessionId, fileName)
	summary := "已查看 /Volumes/Wisdom/MyIdea/CClaw/src/data/ddl_sqlite3.sql(定义 123456 条约束)" +
		"与 config/agentdefault.json(含 987654 个配置项)。"
	result, err := svc.Compact(context.Background(), session, readBack, 1, 4, hCommon.CompactionModeSafeguard,
		fakeSummarizer(summary, nil))
	if err != nil {
		t.Fatalf("Compact 失败: %v", err)
	}
	if result.Degraded {
		t.Errorf("关键信息已保留,不应降级: %+v", result)
	}
	if result.CoversTo != 4 {
		t.Errorf("应覆盖完整区间 1-4,实际到 %d", result.CoversTo)
	}
}

// TestCompactSafeguardSummarizesInBlocks 保全模式下长区间分块摘要(降低长输入丢要点的概率)。
func TestCompactSafeguardSummarizesInBlocks(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)

	var records []hCommon.MemoryRecord
	for i := 1; i <= 60; i++ {
		kind := hCommon.HistoryRecordTypeUser
		if i%2 == 0 {
			kind = hCommon.HistoryRecordTypeAssistant
		}
		records = append(records, record(i, kind, "", strings.Repeat("内容", 30)))
	}
	fileName := writeRecords(t, writer, sessionId, records)
	session := dataobject.TSession{SessionId: sessionId, ChatData: fileName}
	svc := newCompactService()

	calls := 0
	readBack, _ := GetMemoryFileWriter().Read(sessionId, fileName)
	if _, err := svc.Compact(context.Background(), session, readBack, 1, 60, hCommon.CompactionModeSafeguard,
		fakeSummarizer("内容", &calls)); err != nil {
		t.Fatalf("Compact 失败: %v", err)
	}
	if calls < 2 {
		t.Errorf("60 条记录在保全模式下应分块多次摘要,实际调用 %d 次", calls)
	}
}

// TestCompactionModeName 模式名称映射(日志可观测性):0→default、1→safeguard、其余显式标未知。
func TestCompactionModeName(t *testing.T) {
	cases := map[int]string{
		hCommon.CompactionModeDefault:   "default",
		hCommon.CompactionModeSafeguard: "safeguard",
		7:                               "unknown(7)",
	}
	for mode, want := range cases {
		if got := hCommon.CompactionModeName(mode); got != want {
			t.Errorf("CompactionModeName(%d) = %q, want %q", mode, got, want)
		}
	}
}

// TestCompactTruncatesBeforeVerify 回归:保全校验必须针对**真正落盘的那段文本**。
// 旧实现用未截断的完整摘要做校验 → 关键信息在落盘时被截掉也能"校验通过" → 静默丢要点。
func TestCompactTruncatesBeforeVerify(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)
	records := []hCommon.MemoryRecord{
		record(1, hCommon.HistoryRecordTypeUser, "", "配置文件是 config/agentdefault.json,单位编码 100001 "+bulkBody()),
		record(2, hCommon.HistoryRecordTypeAssistant, "", "已记录。"+bulkBody()),
		record(3, hCommon.HistoryRecordTypeUser, "", "继续。"+bulkBody()),
		record(4, hCommon.HistoryRecordTypeAssistant, "", "好的。"+bulkBody()),
		record(5, hCommon.HistoryRecordTypeUser, "", "还有别的吗。"+bulkBody()),
		record(6, hCommon.HistoryRecordTypeAssistant, "", "暂时没有。"+bulkBody()),
	}
	fileName := writeRecords(t, writer, sessionId, records)
	session := dataobject.TSession{SessionId: sessionId, ChatData: fileName}
	svc := newCompactService()
	GetCompactionThrottle().Reset(sessionId)

	// 超长摘要:关键信息只出现在会被截断的尾部
	long := strings.Repeat("填充。", hCommon.MaxSummaryRunes/3+200) // 约 4600 字符 > hCommon.MaxSummaryRunes
	summary := long + "配置为 config/agentdefault.json,单位编码 100001"

	readBack, _ := GetMemoryFileWriter().Read(sessionId, fileName)
	result, err := svc.Compact(context.Background(), session, readBack, 1, 6, hCommon.CompactionModeSafeguard,
		fakeSummarizer(summary, nil))
	if err != nil {
		t.Fatalf("Compact 失败: %v", err)
	}
	if !result.Degraded {
		t.Fatalf("截断后关键信息丢失,必须判为降级: %+v", result)
	}

	after, _ := GetMemoryFileWriter().Read(sessionId, fileName)
	last := after[len(after)-1]
	if !last.IsSummary() {
		t.Fatalf("末条应为摘要记录: %+v", last)
	}
	if got := utf8.RuneCountInString(last.Content); got > hCommon.MaxSummaryRunes+1 { // +1 为截断省略号标记
		t.Errorf("落盘摘要超出上限: %d", got)
	}
	if strings.Contains(last.Content, "agentdefault.json") || strings.Contains(last.Content, "100001") {
		t.Errorf("被截断的关键信息不应出现在落盘摘要中(校验与落盘已对齐): %q", last.Content[:60])
	}
}

// TestCompactThrottledAfterDegrade 回归:降级之后的紧接重试必须被节流 ——
// 否则"降级→后半原文下轮又被裁→再压缩"会退化成**每轮一次** LLM 调用。
func TestCompactThrottledAfterDegrade(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)
	records := []hCommon.MemoryRecord{
		record(1, hCommon.HistoryRecordTypeUser, "", "看 /Volumes/Wisdom/MyIdea/CClaw/src/data/ddl_sqlite3.sql "+bulkBody()),
		record(2, hCommon.HistoryRecordTypeAssistant, "", "共 123456 行。"+bulkBody()),
		record(3, hCommon.HistoryRecordTypeUser, "", "再看 config/agentdefault.json "+bulkBody()),
		record(4, hCommon.HistoryRecordTypeAssistant, "", "共 987654 项。"+bulkBody()),
		record(5, hCommon.HistoryRecordTypeUser, "", "继续"+bulkBody()),
		record(6, hCommon.HistoryRecordTypeAssistant, "", "好"+bulkBody()),
		record(7, hCommon.HistoryRecordTypeUser, "", "还有"+bulkBody()),
		record(8, hCommon.HistoryRecordTypeAssistant, "", "明白"+bulkBody()),
		record(9, hCommon.HistoryRecordTypeUser, "", "继续"+bulkBody()),
		record(10, hCommon.HistoryRecordTypeAssistant, "", "好的"+bulkBody()),
	}
	fileName := writeRecords(t, writer, sessionId, records)
	session := dataobject.TSession{SessionId: sessionId, ChatData: fileName}
	svc := newCompactService()
	GetCompactionThrottle().Reset(sessionId)

	calls := 0
	readBack, _ := GetMemoryFileWriter().Read(sessionId, fileName)
	first, err := svc.Compact(context.Background(), session, readBack, 1, 10, hCommon.CompactionModeSafeguard,
		fakeSummarizer("几个文件已读完。", &calls)) // 刻意不含路径与编号 → 降级
	if err != nil {
		t.Fatalf("首次压缩失败: %v", err)
	}
	if !first.Degraded || calls != 1 {
		t.Fatalf("首次应降级且只调用一次摘要器: %+v calls=%d", first, calls)
	}

	// 紧接着再压同一区间(模拟下一轮):剩余未覆盖记录仍够门槛,必须被**节流**拦住,
	// 否则"降级→后半原文下轮又被裁→再压缩"会退化成每轮一次 LLM 调用。
	readBack, _ = GetMemoryFileWriter().Read(sessionId, fileName)
	second, err := svc.Compact(context.Background(), session, readBack, 1, 10, hCommon.CompactionModeSafeguard,
		fakeSummarizer("第二次摘要。", &calls))
	if err != nil {
		t.Fatalf("二次压缩不应报错: %v", err)
	}
	if second.SummaryIndex != 0 {
		t.Errorf("冷却期内不应再写入摘要: %+v", second)
	}
	if calls != 1 {
		t.Errorf("冷却期内不得再调用摘要器: calls=%d", calls)
	}
}

// TestCompactDefaultSummarizesOnce 默认模式:整段一次性摘要(一次 LLM 调用)。
func TestCompactDefaultSummarizesOnce(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)

	var records []hCommon.MemoryRecord
	for i := 1; i <= 60; i++ {
		records = append(records, record(i, hCommon.HistoryRecordTypeUser, "", strings.Repeat("内容", 30)))
	}
	fileName := writeRecords(t, writer, sessionId, records)
	session := dataobject.TSession{SessionId: sessionId, ChatData: fileName}
	svc := newCompactService()

	calls := 0
	readBack, _ := GetMemoryFileWriter().Read(sessionId, fileName)
	if _, err := svc.Compact(context.Background(), session, readBack, 1, 60, hCommon.CompactionModeDefault,
		fakeSummarizer("内容", &calls)); err != nil {
		t.Fatalf("Compact 失败: %v", err)
	}
	if calls != 1 {
		t.Errorf("默认模式应只调用一次: %d", calls)
	}
}

// TestSummaryIndexMonotonicAfterRestart 回归:压缩摘要只存在于文件里(DB 的 resp_index 不体现),
// 进程重启后新写入的记录**不得**复用摘要已占用的序号。
// 旧实现里 hintIndex>0 时不看文件,重启后会用 DB 的 MAX(resp_index)+1 撞上摘要序号,
// 读取端随之出现两条同序号记录。
func TestSummaryIndexMonotonicAfterRestart(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)
	fileName := writeRecords(t, writer, sessionId, compactRecords()) // 1..12
	session := dataobject.TSession{SessionId: sessionId, ChatData: fileName}
	svc := newCompactService()

	readBack, _ := GetMemoryFileWriter().Read(sessionId, fileName)
	if _, err := svc.Compact(context.Background(), session, readBack, 1, 8, hCommon.CompactionModeDefault,
		fakeSummarizer("摘要", nil)); err != nil {
		t.Fatalf("Compact 失败: %v", err)
	}

	// 模拟进程重启:全新写入器,无缓存;DB 只知道 resp_index 最大为 12
	restarted := &MemoryFileRWriter{nextIndex: make(map[string]int)}
	index, err := restarted.Append(sessionId, fileName, hCommon.HistoryRecordTypeUser, "", "重启后的新问题", "", 13)
	if err != nil {
		t.Fatalf("重启后 Append 失败: %v", err)
	}
	if index != 14 {
		t.Fatalf("摘要占用了 13,新记录应为 14,实际 %d(序号被复用)", index)
	}

	after, _ := GetMemoryFileWriter().Read(sessionId, fileName)
	seen := make(map[int]bool)
	for _, rec := range after {
		if seen[rec.Index] {
			t.Fatalf("出现重复序号 %d:序号契约被破坏", rec.Index)
		}
		seen[rec.Index] = true
	}
	if len(after) != 14 {
		t.Errorf("最终应有 14 条记录,实际 %d", len(after))
	}
}

// TestCompactWithoutFileIsNoop 会话尚未建档(chat_data 为空)时压缩为无操作,不报错。
func TestCompactWithoutFileIsNoop(t *testing.T) {
	svc := newCompactService()
	result, err := svc.Compact(context.Background(), dataobject.TSession{SessionId: "x"}, compactRecords(), 1, 8, 0, fakeSummarizer("摘要", nil))
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if result.SummaryIndex != 0 {
		t.Errorf("无历史文件时不应写入摘要: %+v", result)
	}
}

// TestCompactSummarizerErrorPropagates 摘要器失败要向上返回错误(调用方只记日志,不写坏文件)。
func TestCompactSummarizerErrorPropagates(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)
	fileName := writeRecords(t, writer, sessionId, compactRecords())
	session := dataobject.TSession{SessionId: sessionId, ChatData: fileName}
	svc := newCompactService()

	readBack, _ := GetMemoryFileWriter().Read(sessionId, fileName)
	_, err := svc.Compact(context.Background(), session, readBack, 1, 8, hCommon.CompactionModeDefault,
		func(ctx context.Context, sys, user string) (string, error) { return "", os.ErrDeadlineExceeded })
	if err == nil {
		t.Fatal("摘要器失败应返回错误")
	}

	after, _ := GetMemoryFileWriter().Read(sessionId, fileName)
	if len(after) != 12 {
		t.Errorf("失败时不得写入任何记录: %d", len(after))
	}
}

// TestKeyRetentionRatio 关键信息采样:路径/长数字/文件名即"不能丢"的信息。
func TestKeyRetentionRatio(t *testing.T) {
	records := []hCommon.MemoryRecord{
		record(1, hCommon.HistoryRecordTypeUser, "", "看 /a/b/c.go 与 config/x.json,共 123456 行"),
	}

	ratio, missed := keyRetentionRatio(records, "已查看 /a/b/c.go,包含 123456 行")
	if ratio <= 0 || ratio >= 1 {
		t.Errorf("应部分保留: ratio=%v missed=%v", ratio, missed)
	}
	if len(missed) != 1 || missed[0] != "config/x.json" {
		t.Errorf("未命中的应为 config/x.json: %v", missed)
	}

	if ratio, _ := keyRetentionRatio(records, "已查看 /a/b/c.go 与 config/x.json,共 123456 行"); ratio != 1 {
		t.Errorf("全部保留时 ratio 应为 1: %v", ratio)
	}
	if ratio, _ := keyRetentionRatio([]hCommon.MemoryRecord{record(1, hCommon.HistoryRecordTypeUser, "", "没有关键标记")}, "任意"); ratio != 1 {
		t.Errorf("无关键标记时不应误判丢失: %v", ratio)
	}
}
