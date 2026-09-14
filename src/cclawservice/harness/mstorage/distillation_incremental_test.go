/**
* 蒸馏的增量(watermark)、更新覆盖(supersede)与预算对齐回归测试
*
* 背景:
*   - 旧实现每次都把"全部未覆盖记录"送去蒸馏 → 同一段对话被反复概括,
*     产生近义重复(真实 MEMORY.md 里已出现),文件膨胀后又按"最旧优先"删掉真记忆;
*   - 去重只有精确规范化匹配,同一槽位的取值更新("单位编码 100001" → "…100002")
*     会新旧并存,注入时自相矛盾;
*   - 蒸馏按单文档注入上限写文件,而 MEMORY.md 在注入顺序里排最后,总预算吃紧时先被裁。
**/

package mstorage

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

// readMemoryDoc 读取 agent 的 MEMORY.md(测试辅助)。
func readMemoryDoc(t *testing.T, agent dataobject.TAgent) string {
	t.Helper()
	path, ok := memoryDocPath(agent)
	if !ok {
		t.Fatal("应能解析 MEMORY.md 路径")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 MEMORY.md 失败: %v", err)
	}
	return string(content)
}

// TestDistillSectionHeaderCarriesRecordRange 自动区必须带本次覆盖的记录区间与水位
// (水位是"增量蒸馏"与"注入哪些记录"的依据)。
func TestDistillSectionHeaderCarriesRecordRange(t *testing.T) {
	agent := newDistillAgent(t, "distill_range")
	svc := &MemoryService{enabled: true}

	result, err := svc.Distill(context.Background(), dataobject.TSession{SessionId: "s1"}, distillRecords(), agent,
		fakeSummarizer("- 报告统一使用中文", nil))
	if err != nil {
		t.Fatalf("Distill 失败: %v", err)
	}
	if result.CoversFrom != 1 || result.CoversTo != 6 {
		t.Fatalf("应记录覆盖区间 1-6: %+v", result)
	}

	text := readMemoryDoc(t, agent)
	if !strings.Contains(text, "watermarks=s1:6") {
		t.Errorf("自动区应带会话水位: %s", text)
	}
	if !strings.Contains(text, distillAutoBeginMarkerPrefix) || !strings.Contains(text, distillAutoEndMarker) {
		t.Errorf("自动区应有定界注释: %s", text)
	}
	if got := lastDistilledIndex(text, "s1"); got != 6 {
		t.Errorf("watermark 应为 6,实际 %d", got)
	}
	// 另一个会话不共享水位
	if got := lastDistilledIndex(text, "other"); got != 0 {
		t.Errorf("其它会话水位应为 0,实际 %d", got)
	}
}

// TestDistillIsIncrementalByWatermark 回归:第二轮只蒸馏水位之后的记录,
// 不再把同一段对话重新概括一遍(这是近义重复的根源)。
func TestDistillIsIncrementalByWatermark(t *testing.T) {
	agent := newDistillAgent(t, "distill_incremental")
	svc := &MemoryService{enabled: true}
	session := dataobject.TSession{SessionId: "s1"}

	var captured []string
	summarizer := func(ctx context.Context, sys, user string) (string, error) {
		captured = append(captured, user)
		return "- 事实" + itoa(len(captured)), nil
	}

	// 第一轮:1..6
	if _, err := svc.Distill(context.Background(), session, distillRecords(), agent, summarizer); err != nil {
		t.Fatalf("首轮蒸馏失败: %v", err)
	}
	// 第二轮:同一批记录 + 新增 7..10
	more := distillRecords()
	for i := 7; i <= 10; i++ {
		more = append(more, record(i, hCommon.HistoryRecordTypeUser, "", "新增第"+itoa(i)+"条,以后统一口径"))
	}
	second, err := svc.Distill(context.Background(), session, more, agent, summarizer)
	if err != nil {
		t.Fatalf("次轮蒸馏失败: %v", err)
	}

	if len(captured) != 2 {
		t.Fatalf("应有两次 LLM 调用,实际 %d", len(captured))
	}
	secondPrompt := captured[1]
	if strings.Contains(secondPrompt, "[1] ") || strings.Contains(secondPrompt, "以后所有报告都用中文") {
		t.Errorf("水位之前的记录不应再次送入蒸馏: %q", secondPrompt)
	}
	if !strings.Contains(secondPrompt, "[10]") {
		t.Errorf("水位之后的新记录必须送入蒸馏: %q", secondPrompt)
	}
	if second.CoversFrom != 7 || second.CoversTo != 10 {
		t.Errorf("第二轮区间应为 7-10,实际 %d-%d", second.CoversFrom, second.CoversTo)
	}
}

// TestDistillIncrementalAllowsSmallBatch 水位已推进时,小批次不被"最少记录数"拦掉
// (那个门槛是给"首次蒸馏"用的:批次小是因为增量,不是因为会话太短)。
func TestDistillIncrementalAllowsSmallBatch(t *testing.T) {
	agent := newDistillAgent(t, "distill_small_batch")
	svc := &MemoryService{enabled: true}
	session := dataobject.TSession{SessionId: "s1"}

	if _, err := svc.Distill(context.Background(), session, distillRecords(), agent,
		fakeSummarizer("- 首轮事实", nil)); err != nil {
		t.Fatalf("首轮蒸馏失败: %v", err)
	}

	// 新增 2 条记录(少于 DistillMinRecords)
	more := distillRecords()
	more = append(more,
		record(7, hCommon.HistoryRecordTypeUser, "", "第七条补充:以后报表统一用中文"),
		record(8, hCommon.HistoryRecordTypeAssistant, "", "第八条的补充"),
	)
	calls := 0
	result, err := svc.Distill(context.Background(), session, more, agent, fakeSummarizer("- 增量事实", &calls))
	if err != nil {
		t.Fatalf("增量蒸馏失败: %v", err)
	}
	if calls != 1 {
		t.Errorf("水位后的 2 条记录也应蒸馏: calls=%d result=%+v", calls, result)
	}
	if result.CoversFrom != 7 || result.CoversTo != 8 {
		t.Errorf("增量区间应为 7-8,实际 %d-%d", result.CoversFrom, result.CoversTo)
	}
}

// TestDistillUpdatesSameSlotFact 回归(R3):同一槽位的取值更新必须**替换**旧条目,而不是并存。
func TestDistillUpdatesSameSlotFact(t *testing.T) {
	agent := newDistillAgent(t, "distill_supersede")
	svc := &MemoryService{enabled: true}
	session := dataobject.TSession{SessionId: "s1"}

	if _, err := svc.Distill(context.Background(), session, distillRecords(), agent,
		fakeSummarizer("- 单位编码固定为 100001", nil)); err != nil {
		t.Fatalf("首轮蒸馏失败: %v", err)
	}

	more := distillRecords()
	for i := 7; i <= 12; i++ {
		more = append(more, record(i, hCommon.HistoryRecordTypeUser, "", "新增"+itoa(i)+",以后统一口径"))
	}
	second, err := svc.Distill(context.Background(), session, more, agent,
		fakeSummarizer("- 单位编码固定为 100002", nil))
	if err != nil {
		t.Fatalf("次轮蒸馏失败: %v", err)
	}
	// 自动区是"当前状态列表":同槽位改写记 updated,列表本身仍然只有这一条
	if second.Updated != 1 || len(second.Facts) != 1 {
		t.Fatalf("取值更新应记为 updated=1 且列表仍为 1 条: %+v", second)
	}

	text := readMemoryDoc(t, agent)
	if strings.Contains(text, "100001") {
		t.Errorf("旧取值应已被替换: %s", text)
	}
	if !strings.Contains(text, "100002") {
		t.Errorf("新取值应已写入: %s", text)
	}
	if strings.Count(text, distillAutoBeginMarkerPrefix) != 1 {
		t.Errorf("自动区只能有一个: %s", text)
	}
}

// TestDistillNeverRewritesCuratedRegion "新覆盖旧"只能发生在自动蒸馏区:
// 人工维护区(文件顶部手写的小节)绝不被模型输出改写。
func TestDistillNeverRewritesCuratedRegion(t *testing.T) {
	agent := newDistillAgent(t, "distill_curated")
	path, _ := memoryDocPath(agent)

	curated := "# MEMORY.md\n\n## 单位编码口径\n\n- 单位编码固定为 100001(由财务口径确定)\n"
	if err := os.WriteFile(path, []byte(curated), 0o644); err != nil {
		t.Fatalf("预置人工内容失败: %v", err)
	}

	svc := &MemoryService{enabled: true}
	result, err := svc.Distill(context.Background(), dataobject.TSession{SessionId: "s1"}, distillRecords(), agent,
		fakeSummarizer("- 单位编码固定为 100002", nil))
	if err != nil {
		t.Fatalf("Distill 失败: %v", err)
	}

	text := readMemoryDoc(t, agent)
	if !strings.Contains(text, "100001(由财务口径确定)") {
		t.Errorf("人工维护区不得被改写: %s", text)
	}
	// 人工区已有同槽位事实 → 无法在自动区替换,于是按新增处理(并在文件里保留两条口径,
	// 由人工裁决) —— 这里只要求"人工区原样保留"。
	if result.Updated != 0 {
		t.Errorf("不应替换人工维护区的条目: %+v", result)
	}
}

// TestDistillBatchOverflowLeavesNoGap 溢出时取连续前缀并留下 backlog,下一轮**接着**蒸馏,
// 不允许出现"中间那段永久跳过"的空洞。
func TestDistillBatchOverflowLeavesNoGap(t *testing.T) {
	agent := newDistillAgent(t, "distill_backlog")
	svc := &MemoryService{enabled: true}
	session := dataobject.TSession{SessionId: "s1"}

	// 12 条超长记录:单条被 MemoryExcerptRunes 截到上限后,总量仍远超单次送入预算
	// (信号词放在开头 —— 摘要是取前缀的,信号必须能活过截断)
	var records []hCommon.MemoryRecord
	for i := 1; i <= 12; i++ {
		records = append(records, record(i, hCommon.HistoryRecordTypeUser, "",
			"口径 100001 第"+itoa(i)+"条"+strings.Repeat("详细信息", hCommon.MemoryExcerptRunes)))
	}

	first, err := svc.Distill(context.Background(), session, records, agent, fakeSummarizer("- 事实一", nil))
	if err != nil {
		t.Fatalf("首轮蒸馏失败: %v", err)
	}
	if first.Backlog == 0 {
		t.Fatalf("超长记录应产生 backlog: %+v", first)
	}

	second, err := svc.Distill(context.Background(), session, records, agent, fakeSummarizer("- 事实二", nil))
	if err != nil {
		t.Fatalf("次轮蒸馏失败: %v", err)
	}
	// 关键:下一轮从上一轮的**终点之后**继续,不平移、不跳空
	if second.CoversFrom != first.CoversTo+1 {
		t.Errorf("增量必须连续衔接: 上轮结束于 %d,本轮从 %d 开始", first.CoversTo, second.CoversFrom)
	}
}

// TestDistillBudgetIsBelowInjectionCap 蒸馏的文件预算必须显著小于单文档注入上限 ——
// MEMORY.md 在注入顺序里排最后,总预算吃紧时先被裁;写满到上限等于"写了用不上还删了旧的"。
func TestDistillBudgetIsBelowInjectionCap(t *testing.T) {
	if hCommon.DistillMemoryDocMaxChars >= hCommon.MaxBootstrapFileChars {
		t.Errorf("蒸馏预算(%d)必须小于单文档注入上限(%d)",
			hCommon.DistillMemoryDocMaxChars, hCommon.MaxBootstrapFileChars)
	}
}

// TestParseDistillHeader 标题解析:兼容带/不带记录区间的两种小节,且会话号严格匹配。
func TestParseDistillHeader(t *testing.T) {
	cases := []struct {
		line    string
		session string
		from    int
		to      int
		ok      bool
	}{
		{"## 2026-09-14 10:00:00 | session s1 | records 41-60", "s1", 41, 60, true},
		{"## 2026-09-14 10:00:00 | session s1", "s1", 0, 0, true},
		{"## 2026-09-14 10:00:00 | session s11 | records 1-2", "s11", 1, 2, true},
		{"## 单位编码口径", "", 0, 0, false},
		{"- 普通条目", "", 0, 0, false},
	}
	for _, c := range cases {
		session, from, to, ok := parseDistillHeader(c.line)
		if ok != c.ok || session != c.session || from != c.from || to != c.to {
			t.Errorf("parseDistillHeader(%q) = (%q,%d,%d,%v), want (%q,%d,%d,%v)",
				c.line, session, from, to, ok, c.session, c.from, c.to, c.ok)
		}
	}

	// "sess-1" 不能命中 "sess-11"
	content := "## 2026-09-14 10:00:00 | session sess-11 | records 1-99"
	if got := lastDistilledIndex(content, "sess-1"); got != 0 {
		t.Errorf("会话号必须严格匹配(前缀不算),实际 %d", got)
	}
	_ = regexp.MustCompile
	_ = pCommon.BlankString
}
