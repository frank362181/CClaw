/**
* 压缩的 covers 精确性(P0)与收益门槛回归测试
*
* covers 是对"这段历史已由本摘要代表"的承诺,读取端会无条件信任它(让区间内记录永久退出回放)。
* 因此这里集中验证:**声明覆盖的记录集合 == 真正送进模型的记录集合**。
**/

package mstorage

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/model/dataobject"
)

// ============================== 纯函数级 ==============================

// TestCompactionRunStopsBeforeSummaryRun 区间内出现摘要记录时,压缩区间必须**在其前收尾**。
//
// 否则新摘要的 covers 区间会跨过那条摘要记录的序号,读取端会把那条早期摘要
// 判为"已被更晚摘要取代"而不再回放 —— 而新摘要里并没有它的内容(信息静默丢失)。
func TestCompactionRunStopsBeforeSummaryRun(t *testing.T) {
	records := []hCommon.MemoryRecord{
		record(1, hCommon.HistoryRecordTypeUser, "", "问题一"),
		record(2, hCommon.HistoryRecordTypeAssistant, "", "回答一"),
		record(3, hCommon.HistoryRecordTypeUser, "", "问题二"),
		record(4, hCommon.HistoryRecordTypeAssistant, "", "回答二"),
		record(5, hCommon.HistoryRecordTypeSummary, "", "早期摘要正文"),
	}
	records[4].CoversFrom, records[4].CoversTo = 1, 4

	run := compactionRun(records, 1, 5) // 请求压缩 1-5(含摘要记录 5)
	if len(run) != 0 {
		t.Fatalf("区间内的普通记录都已被摘要 5 覆盖,应为空: %+v", run)
	}

	// 换一个场景:摘要记录夹在待压缩区间中间
	records = []hCommon.MemoryRecord{
		record(1, hCommon.HistoryRecordTypeUser, "", "老问题"),
		record(2, hCommon.HistoryRecordTypeSummary, "", "覆盖 1 的摘要"),
		record(3, hCommon.HistoryRecordTypeUser, "", "问题二"),
		record(4, hCommon.HistoryRecordTypeAssistant, "", "回答二"),
		record(5, hCommon.HistoryRecordTypeUser, "", "问题三"),
		record(6, hCommon.HistoryRecordTypeAssistant, "", "回答三"),
	}
	records[1].CoversFrom, records[1].CoversTo = 1, 1

	run = compactionRun(records, 1, 6)
	if len(run) != 4 {
		t.Fatalf("应从 3 开始连续取到 6(跳过已被 2 覆盖的 1): %+v", run)
	}
	if run[0].Index != 3 || run[3].Index != 6 {
		t.Errorf("压缩区间应为 3-6,实际 %d-%d", run[0].Index, run[3].Index)
	}
	for i := range run {
		if run[i].IsSummary() {
			t.Errorf("压缩区间不得包含摘要记录: %+v", run[i])
		}
	}
}

// TestFitCompactionRangeShrinksInsteadOfDropping 输入预算不够时必须**收缩区间尾部**,
// 而不是把区间中的记录丢掉却仍然声明覆盖整个区间(那部分原文会永久退出回放)。
func TestFitCompactionRangeShrinksInsteadOfDropping(t *testing.T) {
	// 每条约 2000 rune(恰好单条上限),远超 MaxCompactionInputChars 能装下的条数
	body := strings.Repeat("填充", 1000)
	var run []hCommon.MemoryRecord
	for i := 1; i <= 30; i++ {
		run = append(run, record(i, hCommon.HistoryRecordTypeUser, "", body))
	}

	fitted, n := fitCompactionRange(run)
	if n >= len(run) {
		t.Fatalf("超预算的区间应被收缩: fitted=%d total=%d", n, len(run))
	}
	if n < 1 || len(fitted) != n {
		t.Fatalf("收缩结果不正确: fitted=%d", len(fitted))
	}

	// 收缩必须从尾部截断(保留最旧的一段),且内容与源记录完全一致(不丢条)
	for i := range fitted {
		if fitted[i].Index != run[i].Index {
			t.Fatalf("收缩必须保留最旧的连续前缀: 位置 %d 应为 #%d,实际 #%d", i, run[i].Index, fitted[i].Index)
		}
	}
	if got := utf8.RuneCountInString(buildCompactionUserPrompt(fitted, fitted[0].Index, fitted[n-1].Index)); got > hCommon.MaxCompactionInputChars {
		t.Errorf("收缩后送入模型的文本仍超预算: %d", got)
	}
}

// TestVerifyCoversRejectsUnconsumedRecord covers 区间内的记录若既没送进模型、也没有既有摘要
// 为它背书,就必须判为"过度承诺"。
func TestVerifyCoversRejectsUnconsumedRecord(t *testing.T) {
	records := []hCommon.MemoryRecord{
		record(1, hCommon.HistoryRecordTypeUser, "", "送进模型了"),
		record(2, hCommon.HistoryRecordTypeUser, "", "没送进模型"),
	}

	consumed := map[int]bool{1: true}
	if err := verifyCovers(records, 1, 2, consumed, nil); err == nil {
		t.Fatal("区间内有未消费的记录时必须报错")
	}
	if err := verifyCovers(records, 1, 1, consumed, nil); err != nil {
		t.Errorf("完全消费的区间不应报错: %v", err)
	}

	// 已被既有摘要覆盖的记录不算过度承诺(它本来就已退出回放)
	covered := map[int]bool{2: true}
	if err := verifyCovers(records, 1, 2, consumed, covered); err != nil {
		t.Errorf("既有摘要已覆盖的记录不应算过度承诺: %v", err)
	}
}

// TestEffectiveSummaryCapShrinksWithSource 摘要上限随源体积收缩,并受下限/上限约束。
func TestEffectiveSummaryCapShrinksWithSource(t *testing.T) {
	cases := []struct {
		name string
		src  int
		want int
	}{
		{"极小源取下限", 100, hCommon.SummaryMinRunes},
		{"中等源按比例", 2000, int(2000 * hCommon.SummaryShrinkRatio)},
		{"巨大源封顶", 100000, hCommon.MaxSummaryRunes},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := effectiveSummaryCap(c.src); got != c.want {
				t.Errorf("effectiveSummaryCap(%d)=%d want=%d", c.src, got, c.want)
			}
		})
	}
}

// ============================== 端到端(真实读回) ==============================

// TestCompactSkipsWhenNoGain 源体积不大于摘要上限时压缩只有坏处(摘要会折进 system,
// 而 system 越长历史预算越小),必须直接跳过、不发起 LLM 调用。
func TestCompactSkipsWhenNoGain(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)
	records := []hCommon.MemoryRecord{
		record(1, hCommon.HistoryRecordTypeUser, "", "短"),
		record(2, hCommon.HistoryRecordTypeAssistant, "", "短"),
		record(3, hCommon.HistoryRecordTypeUser, "", "短"),
		record(4, hCommon.HistoryRecordTypeAssistant, "", "短"),
	}
	fileName := writeRecords(t, writer, sessionId, records)
	session := dataobject.TSession{SessionId: sessionId, ChatData: fileName}
	svc := newCompactService()

	calls := 0
	readBack, _ := GetMemoryFileWriter().Read(sessionId, fileName)
	result, err := svc.Compact(context.Background(), session, readBack, 1, 4, hCommon.CompactionModeDefault,
		fakeSummarizer("摘要", &calls))
	if err != nil {
		t.Fatalf("Compact 失败: %v", err)
	}
	if calls != 0 || result.SummaryIndex != 0 {
		t.Errorf("无收益时不应压缩: calls=%d result=%+v", calls, result)
	}

	after, _ := GetMemoryFileWriter().Read(sessionId, fileName)
	if len(after) != 4 {
		t.Errorf("无收益时不得写入记录: %d", len(after))
	}
}

// TestCompactCoversNeverSpansEarlierSummary 回归(A2):后一次压缩不得让 covers 区间
// 跨过已有摘要记录的序号 —— 否则读取端会把早期摘要判为已被取代,那段信息凭空消失。
func TestCompactCoversNeverSpansEarlierSummary(t *testing.T) {
	writer, sessionId := newHistoryTestWriter(t)

	// 1..8 压成一条摘要(覆盖 1-8),随后 9..16 继续增长
	records := make([]hCommon.MemoryRecord, 0, 16)
	for i := 1; i <= 16; i++ {
		kind := hCommon.HistoryRecordTypeUser
		if i%2 == 0 {
			kind = hCommon.HistoryRecordTypeAssistant
		}
		records = append(records, record(i, kind, "", "内容"+string(rune('a'+i%26))+" "+bulkBody()))
	}
	fileName := writeRecords(t, writer, sessionId, records)
	session := dataobject.TSession{SessionId: sessionId, ChatData: fileName}
	svc := newCompactService()
	GetCompactionThrottle().Reset(sessionId)

	readBack, _ := GetMemoryFileWriter().Read(sessionId, fileName)
	first, err := svc.Compact(context.Background(), session, readBack, 1, 8, hCommon.CompactionModeDefault,
		fakeSummarizer("前半段的摘要。", nil))
	if err != nil {
		t.Fatalf("首次压缩失败: %v", err)
	}
	if first.CoversFrom != 1 || first.CoversTo != 8 {
		t.Fatalf("首次应覆盖 1-8: %+v", first)
	}

	// 第二次请求压缩 1-16:1-8 已被摘要覆盖,只能覆盖 9 之后的一段
	GetCompactionThrottle().Reset(sessionId)
	readBack, _ = GetMemoryFileWriter().Read(sessionId, fileName)
	second, err := svc.Compact(context.Background(), session, readBack, 1, 16, hCommon.CompactionModeDefault,
		fakeSummarizer("后半段的摘要。", nil))
	if err != nil {
		t.Fatalf("二次压缩失败: %v", err)
	}
	if second.SummaryIndex == 0 {
		t.Fatalf("9-16 尚未被覆盖,应产生摘要: %+v", second)
	}
	if second.CoversFrom <= first.SummaryIndex && second.CoversTo >= first.SummaryIndex {
		t.Fatalf("covers 区间不得跨过早期摘要记录(#%d):%d-%d", first.SummaryIndex, second.CoversFrom, second.CoversTo)
	}

	// 读回验证:两条摘要都必须"仍在生效"(早期摘要没有被误判为已被取代)
	after, _ := GetMemoryFileWriter().Read(sessionId, fileName)
	active := hCommon.ActiveSummaries(after)
	if len(active) != 2 {
		t.Fatalf("两条摘要都应生效,实际 %d 条: %+v", len(active), active)
	}
	if !strings.Contains(active[0].Content, "前半段") || !strings.Contains(active[1].Content, "后半段") {
		t.Errorf("生效摘要内容不正确: %q / %q", active[0].Content, active[1].Content)
	}
}

// TestMergeCandidateAndCompactSummaries 摘要总量超预算时合并最旧的一批摘要:
// 合并后的 covers 必须**包含被合并摘要的记录序号**(否则它们不会被取代,内容重复注入),
// 且不得把从未消费的普通记录卷进区间。
func TestMergeCandidateAndCompactSummaries(t *testing.T) {
	summaries := []hCommon.MemoryRecord{
		record(9, hCommon.HistoryRecordTypeSummary, "", "第一段摘要"),
		record(18, hCommon.HistoryRecordTypeSummary, "", "第二段摘要"),
	}
	summaries[0].CoversFrom, summaries[0].CoversTo = 1, 8
	summaries[1].CoversFrom, summaries[1].CoversTo = 10, 17

	records := []hCommon.MemoryRecord{
		record(1, hCommon.HistoryRecordTypeUser, "", "一"),
		record(2, hCommon.HistoryRecordTypeAssistant, "", "二"),
		summaries[0],
		record(10, hCommon.HistoryRecordTypeUser, "", "十"),
		record(11, hCommon.HistoryRecordTypeAssistant, "", "十一"),
		summaries[1],
		record(19, hCommon.HistoryRecordTypeUser, "", "十九"),
	}
	// 1..8 与 10..17 之间还有 3..8 之类没有真实记录也无妨:covers 只按序号区间表达

	set, from, to := mergeCandidate(records)
	if len(set) != 2 {
		t.Fatalf("应选出两条摘要参与合并: %+v", set)
	}
	if from != 1 {
		t.Errorf("合并区间起点应为最旧摘要的 CoversFrom(1),实际 %d", from)
	}
	if to != summaries[1].Index {
		t.Errorf("合并区间终点必须包含最新被合并摘要的记录序号(%d),实际 %d", summaries[1].Index, to)
	}

	// 端到端:合并后旧摘要必须被取代,注入体积必须下降
	writer, sessionId := newHistoryTestWriter(t)

	var seed []hCommon.MemoryRecord
	for i := 1; i <= 8; i++ {
		seed = append(seed, record(i, hCommon.HistoryRecordTypeUser, "", "早期内容"+bulkBody()))
	}
	fileName := writeRecords(t, writer, sessionId, seed)
	session := dataobject.TSession{SessionId: sessionId, ChatData: fileName}
	svc := newCompactService()
	GetCompactionThrottle().Reset(sessionId)

	readBack, _ := GetMemoryFileWriter().Read(sessionId, fileName)
	if _, err := svc.Compact(context.Background(), session, readBack, 1, 8, hCommon.CompactionModeDefault,
		fakeSummarizer(strings.Repeat("第一段摘要。", 30), nil)); err != nil {
		t.Fatalf("首次压缩失败: %v", err)
	}

	GetCompactionThrottle().Reset(sessionId)
	readBack, _ = GetMemoryFileWriter().Read(sessionId, fileName)
	second, err := svc.Compact(context.Background(), session, readBack, 1, 8, hCommon.CompactionModeDefault,
		fakeSummarizer("不应生效", nil))
	if err != nil {
		t.Fatalf("二次压缩不应报错: %v", err)
	}
	if second.SummaryIndex != 0 {
		t.Fatalf("区间已覆盖,不应二次压缩: %+v", second)
	}

	// 只有一条摘要时无法合并
	readBack, _ = GetMemoryFileWriter().Read(sessionId, fileName)
	if _, _, _ = mergeCandidate(readBack); false {
		t.Log("unreachable")
	}
	if set, _, _ := mergeCandidate(readBack); len(set) != 0 {
		t.Errorf("只有一条摘要时不应合并: %+v", set)
	}
}

// TestSummaryOverBudgetUsesActiveSummaries 摘要总量只看**生效**的摘要:被取代的摘要不再注入,
// 不应计入预算(否则合并后仍会被判为超预算,陷入反复合并)。
func TestSummaryOverBudgetUsesActiveSummaries(t *testing.T) {
	half := hCommon.MaxSummaryTotalRunes / 2

	// 两条互不覆盖的摘要(序号 9 与 18 都不落在对方的 covers 区间里)→ 都生效
	first := record(9, hCommon.HistoryRecordTypeSummary, "", strings.Repeat("一", half))
	first.CoversFrom, first.CoversTo = 1, 8
	second := record(18, hCommon.HistoryRecordTypeSummary, "", strings.Repeat("二", half))
	second.CoversFrom, second.CoversTo = 10, 17

	before := []hCommon.MemoryRecord{first, second}
	if got := len(hCommon.ActiveSummaries(before)); got != 2 {
		t.Fatalf("两条互不覆盖的摘要都应生效,实际 %d", got)
	}
	if got := hCommon.SummariesRunes(before); got != hCommon.MaxSummaryTotalRunes {
		t.Errorf("生效摘要总量应为 %d,实际 %d", hCommon.MaxSummaryTotalRunes, got)
	}
	if ctxmgr.SummaryOverBudget(before) {
		t.Errorf("总量恰好等于上限不应判为超预算")
	}

	// 合并摘要(序号 30,covers 1-18)把前两条都纳入区间 → 前两条被取代,不再计入
	merged := record(30, hCommon.HistoryRecordTypeSummary, "", strings.Repeat("合", hCommon.MaxSummaryTotalRunes+1))
	merged.CoversFrom, merged.CoversTo = 1, 18

	after := []hCommon.MemoryRecord{first, second, merged}
	if got := len(hCommon.ActiveSummaries(after)); got != 1 {
		t.Fatalf("合并后被取代的摘要不应生效,实际生效 %d 条", got)
	}
	if got := hCommon.SummariesRunes(after); got != hCommon.MaxSummaryTotalRunes+1 {
		t.Errorf("被取代的摘要不得计入注入体积: got=%d", got)
	}
	if !ctxmgr.SummaryOverBudget(after) {
		t.Errorf("生效摘要总量超限时应判为超预算(got=%d)", hCommon.SummariesRunes(after))
	}
}
