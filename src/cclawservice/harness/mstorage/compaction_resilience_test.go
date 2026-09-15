/**
* 压缩/蒸馏的"不再静默失效"回归测试(2026-09-14 第二轮)
*
* 覆盖四类此前会静默退化的问题:
*  1. 摘要记录紧跟在不足门槛的残留段之后时,压缩必须跳过残留去压后面的大段(P0-1);
*  2. 蒸馏必须保留**最新**内容、且只看未被摘要代表的记录(P0-2);
*  3. 合并摘要是最激进的一步,必须有保全校验(P1-1);
*  4. 读取端与写端共用同一套"哪些摘要生效"的规则(P2)。
**/

package mstorage

import (
	"context"
	"strings"
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

// ============================== P0-1:压缩区间挑选 ==============================

// TestCompactionRunSkipsUnusableResidual 回归(P0-1):摘要序号紧跟在"不足门槛的残留段"之后
// 是常态(摘要序号在 LLM 调用结束后才分配,期间下一轮记录已写入)。此时必须**跳过残留**,
// 去压屏障后面的大段 —— 旧实现遇到屏障就收尾,导致后面的记录永远够不到、压缩就此停摆。
func TestCompactionRunSkipsUnusableResidual(t *testing.T) {
	records := []hCommon.MemoryRecord{
		record(1, hCommon.HistoryRecordTypeUser, "", "旧"+bulkBody()),
		record(2, hCommon.HistoryRecordTypeAssistant, "", "旧"+bulkBody()),
		record(3, hCommon.HistoryRecordTypeUser, "", "旧"+bulkBody()),
		record(4, hCommon.HistoryRecordTypeAssistant, "", "旧"+bulkBody()),
		record(5, hCommon.HistoryRecordTypeUser, "", "残留"+bulkBody()), // 未被覆盖的残留
		record(6, hCommon.HistoryRecordTypeSummary, "", "覆盖 1-4 的摘要"), // 屏障
	}
	records[5].CoversFrom, records[5].CoversTo = 1, 4
	for i := 7; i <= 20; i++ {
		records = append(records, record(i, hCommon.HistoryRecordTypeUser, "", "新"+bulkBody()))
	}

	// 装配器报告的区间从最旧的未覆盖记录(5)开始
	run := compactionRun(records, 5, 20)
	if len(run) != 14 {
		t.Fatalf("应跳过 1 条残留、取屏障后的 14 条,实际 %d 条: %v", len(run), indexesOf(run))
	}
	if run[0].Index != 7 || run[len(run)-1].Index != 20 {
		t.Errorf("区间应为 7-20,实际 %d-%d", run[0].Index, run[len(run)-1].Index)
	}
	// 不得跨过摘要记录(序号 6 不在区间内)
	for i := range run {
		if run[i].Index == 6 || run[i].IsSummary() {
			t.Fatalf("区间不得包含摘要记录: %+v", run[i])
		}
	}
}

// TestCompactionRunPrefersLargestUsableRun 多个候选段时,取"条数达标且体积最大"的一段
// (体积最大 = 最可能通过收益门槛),其余段留待后续轮次。
func TestCompactionRunPrefersLargestUsableRun(t *testing.T) {
	records := []hCommon.MemoryRecord{
		record(1, hCommon.HistoryRecordTypeUser, "", "小"+bulkBody()), // 段 A:4 条、体积小
		record(2, hCommon.HistoryRecordTypeAssistant, "", "小"+bulkBody()),
		record(3, hCommon.HistoryRecordTypeUser, "", "小"+bulkBody()),
		record(4, hCommon.HistoryRecordTypeAssistant, "", "小"+bulkBody()),
		record(5, hCommon.HistoryRecordTypeSummary, "", "屏障"),
		record(6, hCommon.HistoryRecordTypeUser, "", "大"+bulkBody()+bulkBody()), // 段 B:5 条、体积大
		record(7, hCommon.HistoryRecordTypeAssistant, "", "大"+bulkBody()+bulkBody()),
		record(8, hCommon.HistoryRecordTypeUser, "", "大"+bulkBody()+bulkBody()),
		record(9, hCommon.HistoryRecordTypeAssistant, "", "大"+bulkBody()+bulkBody()),
		record(10, hCommon.HistoryRecordTypeUser, "", "大"+bulkBody()+bulkBody()),
	}
	records[4].CoversFrom, records[4].CoversTo = 100, 100 // 不与 A/B 重叠,仅作为屏障

	run := compactionRun(records, 1, 10)
	if len(run) != 5 || run[0].Index != 6 {
		t.Fatalf("应选体积更大的段 B(6-10),实际 %v", indexesOf(run))
	}

	// 残留段 A 不会被饿死:把 B 标记为已覆盖后,A 就成为唯一候选
	records = append(records, hCommon.MemoryRecord{
		Index: 11, RecordType: hCommon.HistoryRecordTypeSummary, Content: "覆盖 6-10", CoversFrom: 6, CoversTo: 10,
	})
	run = compactionRun(records, 1, 11)
	if len(run) != 4 || run[0].Index != 1 || run[3].Index != 4 {
		t.Fatalf("B 被覆盖后应轮到 A(1-4),实际 %v", indexesOf(run))
	}
}

// ============================== P0-2:蒸馏输入 ==============================

// TestBuildDistillUserPromptKeepsNewest 蒸馏输入的区间与内容必须一致(不得过度声明区间)。
func TestBuildDistillUserPromptBatchRangeMatchesContent(t *testing.T) {
	var records []hCommon.MemoryRecord
	for i := 1; i <= 30; i++ {
		records = append(records, record(i, hCommon.HistoryRecordTypeUser, "", "第"+itoa(i)+"条"+strings.Repeat("内容", 1000)))
	}

	// watermark 机制下,输入取"连续前缀":溢出的部分留在队列里下一轮继续
	batch, lines, backlog := windowPrefix(records, hCommon.DistillMaxInputChars)
	if len(batch) == 0 || batch[0].Index != 1 {
		t.Fatalf("应从最旧开始取连续前缀,实际 %v", indexesOf(batch))
	}
	if len(lines) != len(batch) {
		t.Fatalf("渲染行数应与批次一致: %d vs %d", len(lines), len(batch))
	}
	if backlog != len(records)-len(batch) {
		t.Errorf("backlog 应为剩余条数 %d,实际 %d", len(records)-len(batch), backlog)
	}
	if backlog == 0 {
		t.Fatalf("30 条超长记录不可能一次装下")
	}

	prompt := buildDistillUserPrompt(nil, batch, lines)
	// 表头必须等于实际列出的区间(不得声称看过整个区间)
	headerRange := itoa(batch[0].Index) + "-" + itoa(batch[len(batch)-1].Index)
	if !strings.Contains(prompt, headerRange) {
		t.Errorf("表头区间应等于实际列出的区间(%s): %q", headerRange, prompt[:80])
	}
	if !strings.Contains(prompt, "["+itoa(batch[len(batch)-1].Index)+"]") {
		t.Errorf("批次的最后一条必须被列出")
	}
	if strings.Contains(prompt, "["+itoa(batch[len(batch)-1].Index+1)+"]") {
		t.Errorf("超出批次的记录不得出现在输入里(它们属于 backlog)")
	}
}

// TestWindowPrefixContiguousAndNonEmpty 前缀必须**连续**(不留空洞),且预算再小也至少给 1 条
// (否则队列永远推不动)。
func TestWindowPrefixContiguousAndNonEmpty(t *testing.T) {
	records := []hCommon.MemoryRecord{
		record(1, hCommon.HistoryRecordTypeUser, "", strings.Repeat("长", 5000)),
		record(2, hCommon.HistoryRecordTypeUser, "", strings.Repeat("长", 5000)),
		record(3, hCommon.HistoryRecordTypeUser, "", strings.Repeat("长", 5000)),
	}

	batch, lines, backlog := windowPrefix(records, 0)
	if len(batch) != 1 || batch[0].Index != 1 || backlog != 2 {
		t.Fatalf("预算为 0 时应给 1 条并留下 backlog,实际 batch=%v backlog=%d", indexesOf(batch), backlog)
	}
	if len(lines) != 1 {
		t.Fatalf("渲染行数应与批次一致: %d", len(lines))
	}

	batch, lines, backlog = windowPrefix(records, 20000)
	if len(batch) != 3 || backlog != 0 {
		t.Fatalf("预算充足时应全部取出: %v backlog=%d", indexesOf(batch), backlog)
	}
	if len(lines) != len(records) {
		t.Fatalf("渲染行数应与批次一致: %d", len(lines))
	}
	for i := range batch {
		if batch[i].Index != i+1 {
			t.Errorf("前缀必须连续: 位置 %d 为 #%d", i, batch[i].Index)
		}
	}

	if got, gotLines, gotBacklog := windowPrefix(nil, 100); len(got) != 0 || len(gotLines) != 0 || gotBacklog != 0 {
		t.Errorf("空输入应返回空: %v %d", got, gotBacklog)
	}
}

// TestDistillIgnoresCoveredRecords 回归(P0-2):已被摘要代表的记录既不回放、也不该再参与蒸馏,
// 否则它们是死重(永远占满蒸馏窗口),新内容永远蒸馏不到。
func TestDistillIgnoresCoveredRecords(t *testing.T) {
	agent := newDistillAgent(t, "distill_active")

	records := []hCommon.MemoryRecord{
		record(1, hCommon.HistoryRecordTypeUser, "", "很旧的提问 /old/path/file.go"),
		record(2, hCommon.HistoryRecordTypeAssistant, "", "很旧的回答"),
		record(3, hCommon.HistoryRecordTypeUser, "", "很旧的追问"),
		record(4, hCommon.HistoryRecordTypeAssistant, "", "很旧的收尾"),
		record(5, hCommon.HistoryRecordTypeSummary, "", "覆盖 1-4 的摘要"),
	}
	records[4].CoversFrom, records[4].CoversTo = 1, 4
	for i := 6; i <= 12; i++ {
		records = append(records, record(i, hCommon.HistoryRecordTypeUser, "", "新的提问"+itoa(i)+",以后统一口径"))
	}

	var captured string
	svc := &MemoryService{enabled: true}
	_, err := svc.Distill(context.Background(), dataobject.TSession{SessionId: "sess-active"}, records, agent,
		func(ctx context.Context, sys, user string) (string, error) {
			captured = user
			return "- 新的事实", nil
		})
	if err != nil {
		t.Fatalf("Distill 失败: %v", err)
	}

	if strings.Contains(captured, "/old/path/file.go") || strings.Contains(captured, "很旧的提问") {
		t.Errorf("已被摘要覆盖的记录不应进入蒸馏输入: %q", captured)
	}
	if !strings.Contains(captured, "新的提问12") {
		t.Errorf("最新内容必须进入蒸馏输入: %q", captured)
	}
	if strings.Contains(captured, "覆盖 1-4 的摘要") {
		t.Errorf("摘要记录本身不应进入蒸馏输入(它是压缩产物)")
	}
}

// ============================== P1-1:合并的保全校验 ==============================

// mergeFixture 构造两条互不覆盖、且都含关键信息的摘要(测试辅助)。
func mergeFixture(t *testing.T) (*MemoryService, dataobject.TSession, []hCommon.MemoryRecord) {
	t.Helper()

	writer, sessionId := newHistoryTestWriter(t)
	body := bulkBody()
	var seed []hCommon.MemoryRecord
	for i := 1; i <= 8; i++ {
		seed = append(seed, record(i, hCommon.HistoryRecordTypeUser, "", "早期内容"+body))
	}
	fileName := writeRecords(t, writer, sessionId, seed)

	if _, err := writer.Append(sessionId, fileName, hCommon.HistoryRecordTypeSummary, "",
		"covers: 1-8\n\n第一段:读取 /Volumes/data/ddl_sqlite3.sql,共 123456 行"+strings.Repeat("补充细节。", 60), "", 0); err != nil {
		t.Fatalf("追加第一条摘要失败: %v", err)
	}
	for i := 10; i <= 17; i++ {
		if _, err := writer.Append(sessionId, fileName, hCommon.HistoryRecordTypeUser, "", "中段内容"+body, "", 0); err != nil {
			t.Fatalf("追加失败: %v", err)
		}
	}
	if _, err := writer.Append(sessionId, fileName, hCommon.HistoryRecordTypeSummary, "",
		"covers: 10-17\n\n第二段:配置 config/agentdefault.json,单位编码 100001"+strings.Repeat("补充细节。", 60), "", 0); err != nil {
		t.Fatalf("追加第二条摘要失败: %v", err)
	}

	records, err := GetMemoryFileWriter().Read(sessionId, fileName)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	return newCompactService(), dataobject.TSession{SessionId: sessionId, ChatData: fileName}, records
}

// TestCompactSummariesRefusesLossyMerge 回归(P1-1):合并是最激进的一步(去掉的绝对量最大),
// 必须和普通压缩一样做保全校验 —— 摘要丢了关键信息时**宁可不写**
// (摘要超预算只是多花 token,丢信息不可逆,而 covers 已经承诺取代那些摘要)。
func TestCompactSummariesRefusesLossyMerge(t *testing.T) {
	svc, session, records := mergeFixture(t)
	GetCompactionThrottle().Reset(session.SessionId)

	set, _, _ := mergeCandidate(records)
	if len(set) != 2 {
		t.Fatalf("前置条件:应选出两条摘要参与合并: %+v", set)
	}

	result, err := svc.CompactSummaries(context.Background(), session, records,
		fakeSummarizer("几段内容已合并。", nil)) // 刻意丢掉路径与编号
	if err != nil {
		t.Fatalf("CompactSummaries 不应报错: %v", err)
	}
	if result.SummaryIndex != 0 {
		t.Errorf("失去关键信息的合并不得落盘: %+v", result)
	}

	after, _ := GetMemoryFileWriter().Read(session.SessionId, session.ChatData)
	if len(hCommon.SummariesOf(after)) != 2 {
		t.Errorf("拒绝写入时摘要条数不应变化: %d", len(hCommon.SummariesOf(after)))
	}
}

// TestCompactSummariesMergesWhenKeysKept 关键信息保住时,合并生效:旧摘要被取代、注入体积下降。
func TestCompactSummariesMergesWhenKeysKept(t *testing.T) {
	svc, session, records := mergeFixture(t)
	GetCompactionThrottle().Reset(session.SessionId)

	before := hCommon.SummariesRunes(records)
	merged := "第一段:读取 /Volumes/data/ddl_sqlite3.sql(共 123456 行);" +
		"第二段:配置 config/agentdefault.json(单位编码 100001)。" +
		strings.Repeat("补充说明。", 20)

	result, err := svc.CompactSummaries(context.Background(), session, records, fakeSummarizer(merged, nil))
	if err != nil {
		t.Fatalf("CompactSummaries 失败: %v", err)
	}
	if result.SummaryIndex == 0 || result.Merged != 2 {
		t.Fatalf("关键信息保住时应完成合并: %+v", result)
	}

	after, _ := GetMemoryFileWriter().Read(session.SessionId, session.ChatData)
	active := hCommon.ActiveSummaries(after)
	if len(active) != 1 {
		t.Fatalf("合并后只应有 1 条生效摘要,实际 %d: %+v", len(active), active)
	}
	if got := hCommon.SummariesRunes(after); got >= before {
		t.Errorf("合并后注入体积应下降: before=%d after=%d", before, got)
	}
}

// TestCompactRecordsNoWorkAttempt 回归(P0-1 收尾):"没活干"的早退也必须进入冷却,
// 否则停滞的会话会**每一轮**都起协程 + 整文件读回,而冷却永远不会启动。
func TestCompactRecordsNoWorkAttempt(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)
	fileName := writeRecords(t, writer, sessionId, compactRecords())
	session := dataobject.TSession{SessionId: sessionId, ChatData: fileName}
	svc := newCompactService()
	throttle := GetCompactionThrottle()
	throttle.Reset(sessionId)

	// 先正常压一次,让 1-8 被摘要代表
	readBack, _ := GetMemoryFileWriter().Read(sessionId, fileName)
	if _, err := svc.Compact(context.Background(), session, readBack, 1, 8, hCommon.CompactionModeDefault,
		fakeSummarizer("第一次摘要。", nil)); err != nil {
		t.Fatalf("首次压缩失败: %v", err)
	}

	// 再压同一区间:可压段为空 → 早退,但必须留下"尝试过"的痕迹
	throttle.Reset(sessionId)
	now := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	readBack, _ = GetMemoryFileWriter().Read(sessionId, fileName)
	second, err := svc.Compact(context.Background(), session, readBack, 1, 8, hCommon.CompactionModeDefault,
		fakeSummarizer("不应写入。", nil))
	if err != nil {
		t.Fatalf("二次压缩不应报错: %v", err)
	}
	if second.SummaryIndex != 0 {
		t.Fatalf("已覆盖区间不应写入摘要: %+v", second)
	}
	if throttle.Allow(sessionId, now) {
		t.Errorf("\"没活干\"的早退必须进入冷却(否则每轮都会空转一次协程 + 整文件读回)")
	}
}

// itoa 极简整数转字符串(避免为测试引入 strconv)。
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	negative := v < 0
	if negative {
		v = -v
	}
	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}

// indexesOf 记录序号列表(测试辅助)。
func indexesOf(records []hCommon.MemoryRecord) []int {
	out := make([]int, 0, len(records))
	for i := range records {
		out = append(out, records[i].Index)
	}
	return out
}

// ============================== F6:合并的退一步重试 ==============================

// mergeFixture3 构造三条互不覆盖、且各含不同关键标记的摘要(测试辅助)。
func mergeFixture3(t *testing.T) (*MemoryService, dataobject.TSession, []hCommon.MemoryRecord) {
	t.Helper()

	writer, sessionId := newHistoryTestWriter(t)
	body := bulkBody()

	var seed []hCommon.MemoryRecord
	for i := 1; i <= 8; i++ {
		seed = append(seed, record(i, hCommon.HistoryRecordTypeUser, "", "早期内容"+body))
	}
	fileName := writeRecords(t, writer, sessionId, seed)

	appendSummary := func(from, to int, text string) {
		t.Helper()
		content := "covers: " + itoa(from) + "-" + itoa(to) + "\n\n" + text
		if _, err := writer.Append(sessionId, fileName, hCommon.HistoryRecordTypeSummary, "", content, "", 0); err != nil {
			t.Fatalf("追加摘要(%d-%d)失败: %v", from, to, err)
		}
	}
	fill := func(from, to int) {
		for i := from; i <= to; i++ {
			if _, err := writer.Append(sessionId, fileName, hCommon.HistoryRecordTypeUser, "", "中段内容"+body, "", 0); err != nil {
				t.Fatalf("追加普通记录失败: %v", err)
			}
		}
	}

	appendSummary(1, 8, "第一段:读取 /Volumes/data/ddl_sqlite3.sql,共 123456 行"+strings.Repeat("补充细节。", 60))
	fill(10, 17)
	appendSummary(10, 17, "第二段:配置 config/agentdefault.json,单位编码 100001"+strings.Repeat("补充细节。", 60))
	fill(19, 26)
	appendSummary(19, 26, "第三段:报表模板 report/template.xlsx,编号 654321"+strings.Repeat("补充细节。", 60))

	records, err := GetMemoryFileWriter().Read(sessionId, fileName)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	return newCompactService(), dataobject.TSession{SessionId: sessionId, ChatData: fileName}, records
}

// TestCompactSummariesRetriesWithMinimalSetOnLossyMerge 回归(F6):
// 一次合并 N 条摘要丢掉关键标记时,必须**退回最小集合再试一次**,而不是直接放弃。
//
// 为什么重要:放弃 + "摘要超预算禁用压缩" = 卡死态 —— 既不再产生摘要、摘要也不变小,
// 历史继续被裁却没有任何摘要代表它。合并对象越少,关键标记越容易全部保住,
// 所以"退一步"能在**不降低保全校验标准**的前提下解开卡死。
func TestCompactSummariesRetriesWithMinimalSetOnLossyMerge(t *testing.T) {
	svc, session, records := mergeFixture3(t)
	GetCompactionThrottle().Reset(session.SessionId)
	t.Cleanup(func() { GetCompactionThrottle().Reset(session.SessionId) })

	if got := len(hCommon.SummariesOf(records)); got != 3 {
		t.Fatalf("用例前提:应有 3 条摘要,实际 %d", got)
	}
	before := hCommon.SummariesRunes(records)

	calls := 0
	summarize := func(_ context.Context, _, userPrompt string) (string, error) {
		calls++
		if strings.Count(userPrompt, "(covers ") > hCommon.CompactionMergeMinSummaries {
			// 多集合:刻意丢掉所有关键标记(路径/长数字)→ 保全校验应当拒绝
			return "内容已合并。", nil
		}
		// 最小集合(前两条):保住它们的关键标记
		return "第一段:读取 /Volumes/data/ddl_sqlite3.sql(共 123456 行);" +
			"第二段:配置 config/agentdefault.json(单位编码 100001)。" + strings.Repeat("补充说明。", 20), nil
	}

	result, err := svc.CompactSummaries(context.Background(), session, records, summarize)
	if err != nil {
		t.Fatalf("CompactSummaries 失败: %v", err)
	}
	if calls != 2 {
		t.Fatalf("大集合丢标记后应退回最小集合重试一次(共 2 次调用),实际 %d", calls)
	}
	if result.SummaryIndex == 0 || result.Merged != hCommon.CompactionMergeMinSummaries {
		t.Fatalf("应以最小集合完成合并: %+v", result)
	}

	after, _ := GetMemoryFileWriter().Read(session.SessionId, session.ChatData)
	if got := hCommon.SummariesRunes(after); got >= before {
		t.Errorf("合并后注入体积应下降: before=%d after=%d", before, got)
	}
	if got := len(hCommon.ActiveSummaries(after)); got != 2 {
		t.Errorf("合并 2 条后应剩 2 条生效摘要,实际 %d", got)
	}
}

// TestCompactSummariesGivesUpAndCoolsDownWhenNoLosslessMerge 回归(F6 的兜底):
// 连最小集合都保不住关键标记时不得无限重试 —— WARN + 冷却(每轮最多两次尝试)是唯一可接受的降级,
// 卡死态因此变得**可见**(日志里明确写出"摘要长期超预算、压缩被禁用")。
func TestCompactSummariesGivesUpAndCoolsDownWhenNoLosslessMerge(t *testing.T) {
	svc, session, records := mergeFixture3(t)
	GetCompactionThrottle().Reset(session.SessionId)
	t.Cleanup(func() { GetCompactionThrottle().Reset(session.SessionId) })

	calls := 0
	lossy := fakeSummarizer("内容已合并。", &calls) // 任何规模都丢关键标记

	if _, err := svc.CompactSummaries(context.Background(), session, records, lossy); err != nil {
		t.Fatalf("首次调用不应报错: %v", err)
	}
	if calls != 2 {
		t.Fatalf("两个候选规模都应尝试过,实际 %d", calls)
	}

	// 冷却期内再调用:不得再花调用
	if _, err := svc.CompactSummaries(context.Background(), session, records, lossy); err != nil {
		t.Fatalf("冷却期内调用不应报错: %v", err)
	}
	if calls != 2 {
		t.Fatalf("冷却期内不得重复尝试,实际 %d", calls)
	}
}

// TestCompactSummariesThrottlesRepeatedFailures 回归(F5):合并失败后必须进入冷却。
//
// 为什么必须有:合并的触发条件(摘要总量超预算)一旦成立就是**持续为真**,
// 而失败原因(无可安全合并的集合 / 合并后关键信息丢太多)也往往是持续状态 ——
// 没有冷却就退化成每轮一次 LLM 调用(或每轮一次全量挑选),且永远不会自愈。
//
// 与压缩共用同一张节流表但**分键**(见 mergeThrottleKey):这里同时锁住 Reset 必须两把都清。
func TestCompactSummariesThrottlesRepeatedFailures(t *testing.T) {
	svc, session, records := mergeFixture(t)
	GetCompactionThrottle().Reset(session.SessionId)
	t.Cleanup(func() { GetCompactionThrottle().Reset(session.SessionId) })

	calls := 0
	lossy := fakeSummarizer("几段内容已合并。", &calls) // 刻意丢掉路径与编号 → 保全校验拒绝落盘

	if _, err := svc.CompactSummaries(context.Background(), session, records, lossy); err != nil {
		t.Fatalf("首次调用不应报错: %v", err)
	}
	if calls != 1 {
		t.Fatalf("首次应发起一次合并调用,实际 %d", calls)
	}

	// 冷却期内重复调用:不得再花调用
	for i := 0; i < 2; i++ {
		if _, err := svc.CompactSummaries(context.Background(), session, records, lossy); err != nil {
			t.Fatalf("第 %d 次冷却期内调用不应报错: %v", i+2, err)
		}
	}
	if calls != 1 {
		t.Fatalf("冷却期内不得重复发起合并调用,实际 %d", calls)
	}

	// Reset 必须把压缩与合并两把锁都清掉(否则会出现"重置了却还在冷却")
	GetCompactionThrottle().Reset(session.SessionId)
	if _, err := svc.CompactSummaries(context.Background(), session, records, lossy); err != nil {
		t.Fatalf("Reset 后调用不应报错: %v", err)
	}
	if calls != 2 {
		t.Fatalf("Reset 后应重新尝试合并,实际 %d", calls)
	}
}
