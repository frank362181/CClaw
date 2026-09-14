/**
* 蒸馏加固回归测试(本轮:①A3 信号预筛 ②A4/R1 自动区整体重写 ③A6 回退收敛 ⑤P3 只渲染一次)
*
* 这一批测试锁住的是"蒸馏的调用成本与写入稳定性":
*   - 闲聊批次不该发起 LLM 调用(信号预筛);
*   - 自动区是整体重写的"当前状态列表",反复重写后排版必须收敛(不撑散);
*   - 模型不按格式输出时的回退必须谨慎(不把前言写成长期记忆)。
**/

package mstorage

import (
	"context"
	"os"
	"strings"
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
)

// readMemoryDocIfExists 读取 MEMORY.md;不存在时返回空串(与 readMemoryDoc 的"必须存在"区分开)。
func readMemoryDocIfExists(t *testing.T, agent dataobject.TAgent) string {
	t.Helper()
	path, ok := memoryDocPath(agent)
	if !ok {
		return ""
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(content)
}

// TestDistillSkipsCallWithoutDurableSignal 闲聊批次(只有寒暄/查天气)不发起 LLM 调用 ——
// 长期记忆只看"用户提出的约定/口径/偏好/路径/编号",助手与工具输出里的噪音不算信号。
func TestDistillSkipsCallWithoutDurableSignal(t *testing.T) {
	agent := newDistillAgent(t, "distill_no_signal")
	svc := &MemoryService{enabled: true}

	records := []hCommon.MemoryRecord{
		record(1, hCommon.HistoryRecordTypeUser, "", "今天天气怎么样"),
		record(2, hCommon.HistoryRecordTypeAssistant, "", "晴,26 度"),
		record(3, hCommon.HistoryRecordTypeUser, "", "好的谢谢"),
		record(4, hCommon.HistoryRecordTypeAssistant, "", "不客气"),
		record(5, hCommon.HistoryRecordTypeUser, "", "再看看明天呢"),
		record(6, hCommon.HistoryRecordTypeAssistant, "", "多云"),
	}

	calls := 0
	result, err := svc.Distill(context.Background(), dataobject.TSession{SessionId: "s1"}, records, agent,
		fakeSummarizer("- 某人关心天气", &calls))
	if err != nil {
		t.Fatalf("Distill 失败: %v", err)
	}
	if calls != 0 {
		t.Errorf("无长期信号时不应调用 LLM: calls=%d", calls)
	}
	if result.Path != "" || len(result.Facts) != 0 {
		t.Errorf("跳过时不应产生结果: %+v", result)
	}
	if _, ok := memoryDocPath(agent); !ok {
		t.Fatal("应能解析 MEMORY.md 路径")
	}
	if text := readMemoryDocIfExists(t, agent); text != "" {
		t.Errorf("跳过时不应创建 MEMORY.md: %s", text)
	}

	// 同一批记录里只要有一条用户信号词,就必须调用(宁可多调用一次)
	records[0].Content = "以后所有报告都用中文"
	calls = 0
	if _, err := svc.Distill(context.Background(), dataobject.TSession{SessionId: "s1"}, records, agent,
		fakeSummarizer("- 用户要求报告用中文", &calls)); err != nil {
		t.Fatalf("Distill 失败: %v", err)
	}
	if calls != 1 {
		t.Errorf("出现信号词应触发蒸馏: calls=%d", calls)
	}
}

// TestDistillIsIdempotentWhenNoNewRecords 水位已覆盖全部记录时,同一会话再次蒸馏
// 必须完全不动文件(不调用模型、不重排段落、不追加空行)。
func TestDistillIsIdempotentWhenNoNewRecords(t *testing.T) {
	agent := newDistillAgent(t, "distill_idempotent")
	svc := &MemoryService{enabled: true}
	session := dataobject.TSession{SessionId: "s1"}

	if _, err := svc.Distill(context.Background(), session, distillRecords(), agent,
		fakeSummarizer("- 以后报告统一使用中文\n- 单位编码固定为 100001", nil)); err != nil {
		t.Fatalf("首轮蒸馏失败: %v", err)
	}
	first := readMemoryDoc(t, agent)

	calls := 0
	if _, err := svc.Distill(context.Background(), session, distillRecords(), agent,
		fakeSummarizer("- 变了", &calls)); err != nil {
		t.Fatalf("二次蒸馏失败: %v", err)
	}
	if calls != 0 {
		t.Errorf("无新记录时不应调用 LLM: calls=%d", calls)
	}
	if second := readMemoryDoc(t, agent); second != first {
		t.Errorf("文件必须逐字节不变:\n前:%q\n后:%q", first, second)
	}
}

// TestReplaceAutoMemoryKeepsLayoutStable 反复重写后排版必须收敛:
// 空行不堆积、结尾只有一次换行、自动区只有一个。
func TestReplaceAutoMemoryKeepsLayoutStable(t *testing.T) {
	lines := strings.Split("# MEMORY.md\n\n\n\n## 人工区\n\n- 人工条目\n\n\n", "\n")
	facts := []string{"事实一", "事实二"}

	once := replaceAutoMemory(lines, facts, "s1", 6)
	twice := replaceAutoMemory(strings.Split(once, "\n"), facts, "s1", 6)
	if once != twice {
		t.Errorf("重写应收敛(第二次不应再有变化):\n一次:%q\n二次:%q", once, twice)
	}
	if strings.Contains(once, "\n\n\n") {
		t.Errorf("连续空行应收敛为一个: %q", once)
	}
	if !strings.HasSuffix(once, "\n") || strings.HasSuffix(once, "\n\n") {
		t.Errorf("结尾应恰好一个换行: %q", once)
	}
	if strings.Count(once, distillAutoBeginMarkerPrefix) != 1 {
		t.Errorf("自动区只能有一个: %q", once)
	}
	if !strings.Contains(once, "- 人工条目") {
		t.Errorf("人工内容不得丢失: %q", once)
	}
}

// TestParseDistilledFactsRejectsPreamble 回退路径(模型没给列表)必须谨慎:
// 冒号结尾的前言、过短的标签、引用/分隔线都不算事实。
func TestParseDistilledFactsRejectsPreamble(t *testing.T) {
	for _, bad := range []string{
		"以下是提炼出的长期记忆:",
		"以下是提炼出的长期记忆：",
		"> 引用内容不应作为事实",
		"---",
		"**加粗标题不应作为事实**",
	} {
		if facts := parseDistilledFacts(bad); len(facts) != 0 {
			t.Errorf("前言/噪音不应被当作事实: %q -> %+v", bad, facts)
		}
	}

	// 真正的独立断言(足够长且不以上述形式出现)仍然接受,避免因噎废食
	facts := parseDistilledFacts("用户要求所有对外报告统一使用中文")
	if len(facts) != 1 {
		t.Fatalf("非列表形式的独立断言应被接受: %+v", facts)
	}
}

// TestParseDistilledFactsBulletsWinOverPreamble 有列表时只看列表:前言不进结果。
func TestParseDistilledFactsBulletsWinOverPreamble(t *testing.T) {
	facts := parseDistilledFacts("以下是提炼出的长期记忆:\n- 事实一\n- 事实二")
	if len(facts) != 2 || facts[0] != "事实一" || facts[1] != "事实二" {
		t.Fatalf("应只解析列表项: %+v", facts)
	}
}
