package mstorage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	myCommon "my.favorite/cclawservice/common"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

// readMemoryDoc / itoa / record / fakeSummarizer 等测试辅助见同名文件,此处不再重复定义。

// TestAutoRegionNeverSwallowsCuratedContent 回归:自动区定位必须**只向前吃空行**,
// 人工维护区里紧贴自动区的条目行绝不能被吞进"自动内容"而被替换掉。
func TestAutoRegionNeverSwallowsCuratedContent(t *testing.T) {
	lines := []string{
		"# MEMORY.md",
		"",
		"## 单位口径",
		"",
		"- 单位编码固定为 100001(人工维护)",
		"",
		"## 2026-09-01 10:00:00 | session old | records 1-8",
		"",
		"- 旧条目一",
		"- 旧条目二",
	}
	start, end, ok := autoRegionBounds(lines)
	if !ok {
		t.Fatal("应定位到自动区")
	}
	// 人工条目行(下标 4)必须在区域**之外**
	if start <= 4 {
		t.Fatalf("人工条目行被吞进自动区: start=%d", start)
	}
	if end <= start || end > len(lines) {
		t.Fatalf("区域终点异常: %d-%d", start, end)
	}

	// 端到端:重写自动区后,人工内容必须原样保留
	merged := replaceAutoMemory(lines, []string{"新沉淀"}, "s1", 8)
	if !strings.Contains(merged, "单位编码固定为 100001(人工维护)") {
		t.Errorf("人工维护内容丢失: %s", merged)
	}
	if strings.Contains(merged, "旧条目一") {
		t.Errorf("旧自动内容应已被重写: %s", merged)
	}
	if !strings.Contains(merged, "- 新沉淀") {
		t.Errorf("新条目未写入: %s", merged)
	}

	// 无锚点时不应识别出区域(全人工文件)
	curated := []string{"# MEMORY.md", "", "## 口径", "", "- 某条目"}
	if _, _, ok := autoRegionBounds(curated); ok {
		t.Error("全人工维护的文件不应被识别出自动区")
	}
}

// TestDistillRewritesAutoBlockInsteadOfAppending 回归(A4/R1):自动记忆区是**整体重写**的
// "当前状态列表",不是"每轮追加一个小节 + 满了删最旧" —— 后者会让长期记忆退化成滑动窗口,
// 并把最早写入(往往最长期有效)的事实先删掉。
func TestDistillRewritesAutoBlockInsteadOfAppending(t *testing.T) {
	agent := newDistillAgent(t, "distill_rewrite")
	svc := &MemoryService{enabled: true}
	session := dataobject.TSession{SessionId: "s1"}

	// 第一轮:两条事实
	if _, err := svc.Distill(context.Background(), session, distillRecords(), agent,
		fakeSummarizer("- 以后报告统一使用中文\n- 单位编码固定为 100001", nil)); err != nil {
		t.Fatalf("首轮蒸馏失败: %v", err)
	}

	// 第二轮:模型在"已有记忆"基础上输出更新后的完整列表(改掉旧说法、保留另一条)
	more := distillRecords()
	for i := 7; i <= 12; i++ {
		more = append(more, record(i, hCommon.HistoryRecordTypeUser, "", "新增"+itoa(i)+",以后统一口径"))
	}
	second, err := svc.Distill(context.Background(), session, more, agent,
		fakeSummarizer("- 以后报告统一使用简体中文\n- 单位编码固定为 100001", nil))
	if err != nil {
		t.Fatalf("次轮蒸馏失败: %v", err)
	}

	text := readMemoryDoc(t, agent)
	if strings.Count(text, distillAutoBeginMarkerPrefix) != 1 {
		t.Errorf("自动区必须只有一个: %s", text)
	}
	if strings.Count(text, "- 单位编码固定为 100001") != 1 {
		t.Errorf("同一事实不得重复出现: %s", text)
	}
	if strings.Contains(text, "- 以后报告统一使用中文\n") {
		t.Errorf("被改写的旧说法应已消失: %s", text)
	}
	if !strings.Contains(text, "简体中文") {
		t.Errorf("改写后的新说法应已写入: %s", text)
	}
	if len(second.Facts) != 2 {
		t.Errorf("自动区应恰好两条: %+v", second.Facts)
	}
}

// TestDistillMigratesLegacySections 迁移:旧的"每轮一个小节 + 满了删最旧"格式
// 会在一次重写里被合并进自动记忆区,人工维护区原样保留。
func TestDistillMigratesLegacySections(t *testing.T) {
	agent := newDistillAgent(t, "distill_migrate")
	path, _ := memoryDocPath(agent)

	var sb strings.Builder
	sb.WriteString("# MEMORY.md\n\n## 单位口径\n\n- 单位编码固定为 100001(人工维护)\n\n")
	for i := 0; i < 12; i++ {
		sb.WriteString("## 2026-09-01 10:00:0" + itoa(i%10) + " | session old | records " + itoa(i*2+1) + "-" + itoa(i*2+2) + "\n\n")
		sb.WriteString("- 旧记忆条目" + itoa(i) + "\n\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("预置 MEMORY.md 失败: %v", err)
	}

	// 模型按约定回"更新后的完整列表":12 条旧条目 + 1 条新条目
	var modelOut strings.Builder
	for i := 0; i < 12; i++ {
		modelOut.WriteString("- 旧记忆条目" + itoa(i) + "\n")
	}
	modelOut.WriteString("- 新沉淀的事实条目")

	svc := &MemoryService{enabled: true}
	result, err := svc.Distill(context.Background(), dataobject.TSession{SessionId: "new"}, distillRecords(), agent,
		fakeSummarizer(modelOut.String(), nil))
	if err != nil {
		t.Fatalf("Distill 失败: %v", err)
	}
	if result.Migrated != 12 {
		t.Errorf("应统计到 12 个旧格式小节,实际 %d", result.Migrated)
	}

	text := readMemoryDoc(t, agent)
	if !strings.Contains(text, "单位编码固定为 100001(人工维护)") {
		t.Errorf("人工维护区必须原样保留: %s", text)
	}
	if distillSectionHeaderPattern.MatchString(text) {
		t.Errorf("旧格式小节应已并入自动区: %s", text)
	}
	if !strings.Contains(text, distillAutoBeginMarkerPrefix) {
		t.Errorf("应写入自动区定界注释: %s", text)
	}
	for _, want := range []string{"旧记忆条目0", "旧记忆条目11", "新沉淀的事实条目"} {
		if !strings.Contains(text, want) {
			t.Errorf("条目丢失: %s\n%s", want, text)
		}
	}
	if result.Removed != 0 {
		t.Errorf("模型回全了列表,不应有 removed: %+v", result)
	}
}

// TestDistillRefusesSuspiciousRewrite 安全阀:模型把旧条目丢掉超过一半时不可信,
// 必须降级为"保留旧条目 + 追加新条目",绝不因一次可疑重写丢掉长期记忆。
func TestDistillRefusesSuspiciousRewrite(t *testing.T) {
	agent := newDistillAgent(t, "distill_valve")
	svc := &MemoryService{enabled: true}
	session := dataobject.TSession{SessionId: "s1"}

	if _, err := svc.Distill(context.Background(), session, distillRecords(), agent,
		fakeSummarizer("- 以后报告统一使用中文\n- 单位编码固定为 100001\n- 交付物统一放在 workspace/report", nil)); err != nil {
		t.Fatalf("首轮蒸馏失败: %v", err)
	}

	more := distillRecords()
	for i := 7; i <= 12; i++ {
		more = append(more, record(i, hCommon.HistoryRecordTypeUser, "", "新增"+itoa(i)+",以后统一口径"))
	}
	// 模型在一份 3 条的旧列表上只回了一条(丢了 2/3)→ 不可信
	second, err := svc.Distill(context.Background(), session, more, agent,
		fakeSummarizer("- 只有一条新事实,以后统一口径", nil))
	if err != nil {
		t.Fatalf("次轮蒸馏失败: %v", err)
	}

	text := readMemoryDoc(t, agent)
	for _, want := range []string{"统一使用中文", "100001", "workspace/report", "只有一条新事实"} {
		if !strings.Contains(text, want) {
			t.Errorf("降级后不得丢条目(%s):\n%s", want, text)
		}
	}
	if second.Removed != 0 {
		t.Errorf("降级路径不应上报 removed: %+v", second)
	}
}

// newDistillAgent 构造一个真实的 Agent 工作区(测试辅助),并注册清理。
// 沿用生产路径 BuildAgentBootstrapRoot,确保测的是真实解析逻辑。
func newDistillAgent(t *testing.T, agentDir string) dataobject.TAgent {
	t.Helper()

	owner := "100001"
	agent := dataobject.TAgent{AgentId: "agent_dt", AgentDir: agentDir, OwnerId: owner}

	root := filepath.Join(pCommon.GetExecutableDir(), myCommon.AgentBootstrapDir, owner, agentDir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("创建工作区失败: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	return agent
}

// distillRecords 构造一段足够长的会话记录(测试辅助)。
func distillRecords() []hCommon.MemoryRecord {
	return []hCommon.MemoryRecord{
		record(1, hCommon.HistoryRecordTypeUser, "", "以后所有报告都用中文,并且先给结论。"),
		record(2, hCommon.HistoryRecordTypeAssistant, "", "好的,已记住你的偏好。"),
		record(3, hCommon.HistoryRecordTypeUser, "", "项目路径是 /Volumes/Wisdom/MyIdea/CClaw/src"),
		record(4, hCommon.HistoryRecordTypeAssistant, "", "明白。"),
		record(5, hCommon.HistoryRecordTypeUser, "", "单位编码固定为 100001"),
		record(6, hCommon.HistoryRecordTypeAssistant, "", "已记录。"),
	}
}

// TestDistillWritesFactsToMemoryDoc 蒸馏应把事实写入工作区 MEMORY.md 的时间戳小节。
func TestDistillWritesFactsToMemoryDoc(t *testing.T) {
	agent := newDistillAgent(t, "distill_basic")
	svc := &MemoryService{enabled: true}

	facts := "- 用户偏好:报告使用中文且先给结论\n- 项目路径为 /Volumes/Wisdom/MyIdea/CClaw/src\n- 单位编码固定为 100001"
	result, err := svc.Distill(context.Background(), dataobject.TSession{SessionId: "sess-1"}, distillRecords(), agent,
		fakeSummarizer(facts, nil))
	if err != nil {
		t.Fatalf("Distill 失败: %v", err)
	}
	if len(result.Facts) != 3 {
		t.Fatalf("应写入 3 条事实: %+v", result)
	}

	path, ok := memoryDocPath(agent)
	if !ok {
		t.Fatal("应能解析 MEMORY.md 路径")
	}
	if result.Path != path {
		t.Errorf("路径不一致: %s vs %s", result.Path, path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("MEMORY.md 未写入: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, distillAutoBeginMarkerPrefix) || !strings.Contains(text, distillAutoEndMarker) {
		t.Errorf("应包含自动记忆区定界注释: %s", text)
	}
	if got := lastDistilledIndex(text, "sess-1"); got != 6 {
		t.Errorf("水位线应推进到 6,实际 %d: %s", got, text)
	}
	for _, fact := range []string{"先给结论", "/Volumes/Wisdom/MyIdea/CClaw/src", "100001"} {
		if !strings.Contains(text, fact) {
			t.Errorf("事实丢失: %s\n%s", fact, text)
		}
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Errorf("临时文件应已改名,不应残留")
	}
}

// TestDistillDeduplicatesFacts 同一事实重复蒸馏时不应重复写入。
func TestDistillDeduplicatesFacts(t *testing.T) {
	agent := newDistillAgent(t, "distill_dedup")
	svc := &MemoryService{enabled: true}

	facts := "- 用户偏好报告使用中文,且先给结论。"
	if _, err := svc.Distill(context.Background(), dataobject.TSession{SessionId: "s1"}, distillRecords(), agent,
		fakeSummarizer(facts, nil)); err != nil {
		t.Fatalf("首次蒸馏失败: %v", err)
	}

	// 第二次:措辞略有差异(空格/标点/大小写),规范化后应判定为重复
	second, err := svc.Distill(context.Background(), dataobject.TSession{SessionId: "s2"}, distillRecords(), agent,
		fakeSummarizer("-   用户偏好报告使用中文，且先给结论 ", nil))
	if err != nil {
		t.Fatalf("二次蒸馏失败: %v", err)
	}
	// 规范化后同义(只是标点/空格不同)→ 不算内容变化,但文件里只能留一条
	if len(second.Facts) != 1 || second.Updated != 0 {
		t.Errorf("同槽位同义改写:列表仍为 1 条且不计 updated: %+v", second)
	}

	path, _ := memoryDocPath(agent)
	content, _ := os.ReadFile(path)
	if n := strings.Count(string(content), "用户偏好报告使用中文"); n != 1 {
		t.Errorf("重复事实不应出现两次(实际 %d 次): %s", n, content)
	}
	if !strings.Contains(string(content), "watermarks=s1:") {
		t.Errorf("水位线应保留原会话: %s", content)
	}
}

// TestDistillNoneOutputWritesNothing 模型判定"无长期价值"时不创建文件。
func TestDistillNoneOutputWritesNothing(t *testing.T) {
	agent := newDistillAgent(t, "distill_none")
	svc := &MemoryService{enabled: true}

	result, err := svc.Distill(context.Background(), dataobject.TSession{SessionId: "s1"}, distillRecords(), agent,
		fakeSummarizer("NONE", nil))
	if err != nil {
		t.Fatalf("Distill 失败: %v", err)
	}
	if len(result.Facts) != 0 {
		t.Errorf("NONE 不应写入任何事实: %+v", result)
	}
	path, _ := memoryDocPath(agent)
	if _, err := os.Stat(path); err == nil {
		t.Errorf("无事实时不应创建 MEMORY.md")
	}
}

// TestDistillSkipsShortConversation 记录太少的会话不做蒸馏(避免无意义的 LLM 调用)。
func TestDistillSkipsShortConversation(t *testing.T) {
	agent := newDistillAgent(t, "distill_short")
	svc := &MemoryService{enabled: true}

	calls := 0
	result, err := svc.Distill(context.Background(), dataobject.TSession{SessionId: "s1"}, distillRecords()[:2], agent,
		fakeSummarizer("- 事实", &calls))
	if err != nil {
		t.Fatalf("Distill 失败: %v", err)
	}
	if calls != 0 || len(result.Facts) != 0 {
		t.Errorf("短会话不应触发蒸馏: calls=%d result=%+v", calls, result)
	}
}

// TestDistillSkipsMissingWorkspace Agent 工作区不存在时跳过(不隐式创建工作区)。
func TestDistillSkipsMissingWorkspace(t *testing.T) {
	svc := &MemoryService{enabled: true}
	agent := dataobject.TAgent{AgentId: "agent_missing", AgentDir: "no_such_workspace", OwnerId: "100001"}

	calls := 0
	result, err := svc.Distill(context.Background(), dataobject.TSession{SessionId: "s1"}, distillRecords(), agent,
		fakeSummarizer("- 事实", &calls))
	if err != nil {
		t.Fatalf("工作区缺失不应报错: %v", err)
	}
	if calls != 0 || result.Path != "" {
		t.Errorf("工作区缺失应直接跳过: calls=%d result=%+v", calls, result)
	}
}

// TestParseDistilledFacts 解析模型输出:仅取 "- " 开头的行,过滤 NONE 与空行。
func TestParseDistilledFacts(t *testing.T) {
	facts := parseDistilledFacts("说明文字\n- 事实一\n- 事实二\n\nNONE\n- 事实三")
	if len(facts) != 3 {
		t.Fatalf("应解析出 3 条: %+v", facts)
	}
	if facts[0] != "事实一" || facts[2] != "事实三" {
		t.Errorf("条目内容不正确: %+v", facts)
	}
	if got := parseDistilledFacts("NONE"); got != nil {
		t.Errorf("NONE 应返回空: %+v", got)
	}
	if got := parseDistilledFacts("  "); got != nil {
		t.Errorf("空输出应返回空: %+v", got)
	}
}

// TestNormalizeFact 事实规范化:忽略空白、常见标点与大小写差异。
// 说明:NormalizeFact 已上移到 platformutility/common(跨模块共用),此处按新归属调用。
func TestNormalizeFact(t *testing.T) {
	a := pCommon.NormalizeFact("用户偏好:报告用中文,先给结论。")
	b := pCommon.NormalizeFact(" 用户偏好：报告用中文，先给结论 ")
	if a != b {
		t.Errorf("规范化后应相同: %q vs %q", a, b)
	}
	if pCommon.NormalizeFact("  ") != "" {
		t.Errorf("空白应规范化为空串")
	}
}
