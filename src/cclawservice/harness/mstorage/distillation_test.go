package mstorage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	myCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

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
func distillRecords() []MemoryRecord {
	return []MemoryRecord{
		record(1, HistoryRecordUser, "", "以后所有报告都用中文,并且先给结论。"),
		record(2, HistoryRecordAssistant, "", "好的,已记住你的偏好。"),
		record(3, HistoryRecordUser, "", "项目路径是 /Volumes/Wisdom/MyIdea/CClaw/src"),
		record(4, HistoryRecordAssistant, "", "明白。"),
		record(5, HistoryRecordUser, "", "单位编码固定为 100001"),
		record(6, HistoryRecordAssistant, "", "已记录。"),
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

	path, ok := MemoryDocPath(agent)
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
	if !strings.Contains(text, "## ") || !strings.Contains(text, "session sess-1") {
		t.Errorf("应包含带会话标识的时间戳小节: %s", text)
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
	if len(second.Facts) != 0 || second.Skipped != 1 {
		t.Errorf("重复事实应被跳过: %+v", second)
	}

	path, _ := MemoryDocPath(agent)
	content, _ := os.ReadFile(path)
	if strings.Contains(string(content), "session s2") {
		t.Errorf("重复事实不应新建小节: %s", content)
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
	path, _ := MemoryDocPath(agent)
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

// TestDistillPrunesOldestSections 超出预算时从最旧的小节开始裁剪,最新小节必须保留。
func TestDistillPrunesOldestSections(t *testing.T) {
	agent := newDistillAgent(t, "distill_prune")
	path, _ := MemoryDocPath(agent)

	// 预置多个旧小节,总量已明显超过注入预算
	var sb strings.Builder
	for i := 0; i < 12; i++ {
		sb.WriteString("## 2026-09-01 10:00:0")
		sb.WriteString(string(rune('0' + i%10)))
		sb.WriteString(" | session old\n\n")
		sb.WriteString("- 旧记忆条目 " + strings.Repeat("内容", 900) + "\n\n")
	}
	oldest := "## 2026-09-01 10:00:00 | session old"
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("预置 MEMORY.md 失败: %v", err)
	}

	svc := &MemoryService{enabled: true}
	result, err := svc.Distill(context.Background(), dataobject.TSession{SessionId: "new"}, distillRecords(), agent,
		fakeSummarizer("- 最新的事实条目", nil))
	if err != nil {
		t.Fatalf("Distill 失败: %v", err)
	}
	if result.Pruned == 0 {
		t.Fatalf("超预算时应裁剪旧小节: %+v", result)
	}

	content, _ := os.ReadFile(path)
	text := string(content)
	if !strings.Contains(text, "最新的事实条目") || !strings.Contains(text, "session new") {
		t.Errorf("最新小节必须保留: %s", text)
	}
	if strings.HasPrefix(strings.TrimSpace(text), oldest) && result.Pruned > 0 {
		t.Errorf("最旧小节应已被裁剪: %s", text[:120])
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
