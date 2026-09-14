/**
* 蒸馏的迁移期与并发语义回归测试
*
* 三条都是"错了不会报错、只会静默做错事"的规则:
*   1. 迁移期(文件里还只有旧格式时间戳小节)必须信任模型的范围判断 ——
*      否则作用域从"用户偏好"收紧到"单位/agent 级事实"之后,旧越界内容会被安全阀永远钉在
*      共享文件里(实测 default/MEMORY.md 就是 10 条个人偏好 + 天气历史);
*   2. 迁移期模型明确说 NONE 时可以清空条目,但**水位必须留下**(否则下一轮重复蒸馏同一段);
*   3. "被模型丢弃"只能对**提示词快照**里的条目计数:并发蒸馏时另一个会话写入的新条目
*      模型根本没看过,按文件当前内容计数会把它们误删;
*   4. 短条目(不足 DistillSimilarFactMinRunes)不能用相似度判定,必须靠规范化相等 ——
*      否则会被重复写入、且"被丢弃"计数虚高。
**/

package mstorage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
)

// legacyMemoryDoc 写一份"旧格式"MEMORY.md:人工维护区 + n 个时间戳小节。
func legacyMemoryDoc(t *testing.T, agent dataobject.TAgent, sections int, body func(i int) string) string {
	t.Helper()

	path, ok := memoryDocPath(agent)
	if !ok {
		t.Fatal("应能解析 MEMORY.md 路径")
	}

	var sb strings.Builder
	sb.WriteString("# MEMORY.md\n\n## 单位口径(人工维护)\n\n- 单位编码统一为 6 位数字\n\n")
	for i := 0; i < sections; i++ {
		sb.WriteString("## 2026-09-01 10:00:0" + itoa(i%10) + " | session old\n\n")
		sb.WriteString("- " + body(i) + "\n\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("预置 MEMORY.md 失败: %v", err)
	}
	return path
}

// TestDistillMigrationAcceptsModelScope 迁移期:模型把越界旧条目删掉是正确的,安全阀不得驳回。
func TestDistillMigrationAcceptsModelScope(t *testing.T) {
	agent := newDistillAgent(t, "distill_migrate_scope")

	// 真实形态的越界内容(取自实际写进过 default/MEMORY.md 的旧条目):
	// 必须彼此不相似,否则会被当"同一槽位"先合并掉,测不到 6 条的量级
	personal := []string{
		"用户的幸运数字是12345",
		"用户最喜欢的花是光木兰",
		"用户最喜欢的城市是杭州",
		"用户查询过天气的城市包括郴州",
		"用户偏好结构化清晰的天气报告",
		"用户会重复确认自己的幸运数字",
	}
	legacyMemoryDoc(t, agent, len(personal), func(i int) string { return personal[i] })

	svc := &MemoryService{enabled: true}
	result, err := svc.Distill(context.Background(), dataobject.TSession{SessionId: "s1"}, distillRecords(), agent,
		fakeSummarizer("- 以后所有对外报告统一使用中文", nil))
	if err != nil {
		t.Fatalf("Distill 失败: %v", err)
	}

	text := readMemoryDoc(t, agent)
	if !strings.Contains(text, "以后所有对外报告统一使用中文") {
		t.Errorf("新条目必须写入: %s", text)
	}
	for _, fact := range personal {
		if strings.Contains(text, fact) {
			t.Errorf("迁移期模型判定越界的旧条目应被删除(%s): %s", fact, text)
		}
	}
	if !strings.Contains(text, "单位编码统一为 6 位数字") {
		t.Errorf("人工维护区必须原样保留: %s", text)
	}
	if result.Migrated != len(personal) {
		t.Errorf("应统计到 %d 个旧格式小节,实际 %d", len(personal), result.Migrated)
	}
	// 被删掉的旧条目计入 removed(可观测),但不再被安全阀驳回
	if result.Removed != len(personal) {
		t.Errorf("迁移期应如实上报 removed=%d,实际 %d", len(personal), result.Removed)
	}
}

// TestDistillMigrationExplicitNoneClearsLegacy 迁移期模型明确 NONE:条目清空,水位留下。
func TestDistillMigrationExplicitNoneClearsLegacy(t *testing.T) {
	agent := newDistillAgent(t, "distill_migrate_none")
	legacyMemoryDoc(t, agent, 4, func(i int) string { return "一次性的天气查询记录" + itoa(i) })

	svc := &MemoryService{enabled: true}
	if _, err := svc.Distill(context.Background(), dataobject.TSession{SessionId: "s1"}, distillRecords(), agent,
		fakeSummarizer(hCommon.DistillNoneMark, nil)); err != nil {
		t.Fatalf("Distill 失败: %v", err)
	}

	text := readMemoryDoc(t, agent)
	for i := 0; i < 4; i++ {
		if strings.Contains(text, "天气查询记录"+itoa(i)) {
			t.Errorf("模型明确 NONE 时旧格式的越界条目应被清空(#%d): %s", i, text)
		}
	}
	if !strings.Contains(text, distillAutoBeginMarkerPrefix) || !strings.Contains(text, distillAutoEndMarker) {
		t.Errorf("应留下空自动区(定界注释): %s", text)
	}
	if !strings.Contains(text, "单位编码统一为 6 位数字") {
		t.Errorf("人工维护区必须原样保留: %s", text)
	}
	// 水位必须保留:否则下一轮会把同一段对话重新蒸馏一遍
	if got := lastDistilledIndex(text, "s1"); got != 6 {
		t.Errorf("清空条目后水位仍应推进到 6,实际 %d: %s", got, text)
	}
	if facts, _ := existingMemoryFacts(strings.Split(text, "\n")); len(facts) != 0 {
		t.Errorf("条目应已清空: %+v", facts)
	}
}

// TestDistillKeepsFactsAddedAfterPromptSnapshot 并发:本轮开始之后另一个会话写入的条目必须保留。
func TestDistillKeepsFactsAddedAfterPromptSnapshot(t *testing.T) {
	agent := newDistillAgent(t, "distill_snapshot")
	svc := &MemoryService{enabled: true}
	session := dataobject.TSession{SessionId: "s1"}

	// 首轮:文件里先有 3 条(模拟"另一个会话之前已蒸馏过")
	if _, err := svc.Distill(context.Background(), session, distillRecords(), agent,
		fakeSummarizer("- 以后报告统一使用中文\n- 单位编码固定为 100001\n- 交付物统一放在 workspace/report", nil)); err != nil {
		t.Fatalf("首轮蒸馏失败: %v", err)
	}

	// 次轮:在模型返回**之前**(即提示词快照之后)另一个会话写入一条新条目
	injected := "以后所有金额统一保留两位小数"
	var injectedOnce bool
	summarize := func(ctx context.Context, sys, user string) (string, error) {
		if !injectedOnce {
			injectedOnce = true
			injectFactIntoAutoBlock(t, agent, injected)
		}
		return "- 以后报告统一使用简体中文\n- 单位编码固定为 100001\n- 交付物统一放在 workspace/report", nil
	}

	more := distillRecords()
	for i := 7; i <= 12; i++ {
		more = append(more, record(i, hCommon.HistoryRecordTypeUser, "", "新增"+itoa(i)+",以后统一口径"))
	}
	second, err := svc.Distill(context.Background(), dataobject.TSession{SessionId: "s2"}, more, agent, summarize)
	if err != nil {
		t.Fatalf("次轮蒸馏失败: %v", err)
	}

	text := readMemoryDoc(t, agent)
	if !strings.Contains(text, injected) {
		t.Errorf("快照之后新增的条目不得被删除: %s", text)
	}
	if second.Removed != 0 {
		t.Errorf("快照之外的条目不得计入 removed: %+v", second)
	}
	if !strings.Contains(text, "简体中文") {
		t.Errorf("模型对快照内条目的改写应生效: %s", text)
	}
}

// TestDistillShortFactsMatchByNormalization 短条目靠规范化相等判定同一槽位。
func TestDistillShortFactsMatchByNormalization(t *testing.T) {
	agent := newDistillAgent(t, "distill_short_fact")
	svc := &MemoryService{enabled: true}

	const short = "口径按月末"
	if _, err := svc.Distill(context.Background(), dataobject.TSession{SessionId: "s1"}, distillRecords(), agent,
		fakeSummarizer("- "+short, nil)); err != nil {
		t.Fatalf("首轮蒸馏失败: %v", err)
	}

	// 次轮模型原样回同一条(标点略变)
	second, err := svc.Distill(context.Background(), dataobject.TSession{SessionId: "s2"}, distillRecords(), agent,
		fakeSummarizer("- "+short+"。", nil))
	if err != nil {
		t.Fatalf("次轮蒸馏失败: %v", err)
	}

	text := readMemoryDoc(t, agent)
	if n := strings.Count(text, short); n != 1 {
		t.Errorf("短条目不得重复写入(实际 %d 次): %s", n, text)
	}
	if second.Removed != 0 {
		t.Errorf("原样回同一条不应计入 removed: %+v", second)
	}
	if len(second.Facts) != 1 {
		t.Errorf("列表应仍为 1 条: %+v", second)
	}
}

// injectFactIntoAutoBlock 模拟"另一个会话"往自动区里写一条条目(测试辅助)。
func injectFactIntoAutoBlock(t *testing.T, agent dataobject.TAgent, fact string) {
	t.Helper()

	path, ok := memoryDocPath(agent)
	if !ok {
		t.Fatal("应能解析 MEMORY.md 路径")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 MEMORY.md 失败: %v", err)
	}
	text := string(content)
	updated := strings.Replace(text, distillAutoEndMarker, "- "+fact+"\n"+distillAutoEndMarker, 1)
	if updated == text {
		t.Fatalf("自动区未找到,无法注入: %s", text)
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatalf("注入条目失败: %v", err)
	}
}

var _ = filepath.Join
