package ctxmgr

import (
	"strings"
	"testing"

	"my.favorite/cclawservice/harness/mstorage"
	"my.favorite/cclawservice/llmproto"
	"my.favorite/cclawservice/model/dataobject"
)

// record 构造一条历史记录(测试辅助)
func record(index int, recordType, title, content string) mstorage.MemoryRecord {
	return mstorage.MemoryRecord{Index: index, RecordType: recordType, Title: title, Content: content}
}

// summaryRecord 构造一条摘要记录(测试辅助)
func summaryRecord(index, from, to int, content string) mstorage.MemoryRecord {
	return mstorage.MemoryRecord{Index: index, RecordType: mstorage.MemoryRecordSummary, Content: content, CoversFrom: from, CoversTo: to}
}

// roles 提取消息角色序列(测试辅助)
func roles(messages []llmproto.ChatMessage) []string {
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		out = append(out, msg.Role)
	}
	return out
}

// TestAssembleContextWithoutHistory 无历史时退化为 [system, 本轮问题](与阶段1行为一致)。
func TestAssembleContextWithoutHistory(t *testing.T) {
	result := AssembleContext("你是助手。", nil, "你好", DefaultContextPolicy())

	if got := strings.Join(roles(result.Messages), ","); got != "system,user" {
		t.Fatalf("角色序列应为 system,user,实际 %s", got)
	}
	if result.Messages[0].Content != "你是助手。" {
		t.Errorf("system 内容不应被改动: %q", result.Messages[0].Content)
	}
	if result.Messages[1].Content != "你好" {
		t.Errorf("本轮用户消息不正确: %q", result.Messages[1].Content)
	}
	if result.Trimmed || result.NeedCompaction {
		t.Errorf("无历史时不应判定为已裁剪/需压缩: %+v", result)
	}
}

// TestAssembleContextHistoryOrder 历史按序回放,且以本轮问题结尾(不重复上一轮的提问)。
func TestAssembleContextHistoryOrder(t *testing.T) {
	records := []mstorage.MemoryRecord{
		record(1, mstorage.HistoryRecordUser, "", "第一个问题"),
		record(2, mstorage.HistoryRecordAssistant, "", "第一个回答"),
		record(3, mstorage.HistoryRecordUser, "", "第二个问题"),
		record(4, mstorage.HistoryRecordAssistant, "", "第二个回答"),
	}

	result := AssembleContext("SYS", records, "第三个问题", DefaultContextPolicy())

	if got := strings.Join(roles(result.Messages), ","); got != "system,user,assistant,user,assistant,user" {
		t.Fatalf("角色序列不正确: %s", got)
	}
	if result.Messages[1].Content != "第一个问题" || result.Messages[5].Content != "第三个问题" {
		t.Errorf("历史与本轮内容顺序不正确: %+v", result.Messages)
	}
}

// TestAssembleContextToolResultsFoldedIntoAssistant 工具结果必须折叠进同轮助手消息,
// 且不产生 role=tool 的孤立消息(避免合成 tool_call_id 破坏协议)。
func TestAssembleContextToolResultsFoldedIntoAssistant(t *testing.T) {
	records := []mstorage.MemoryRecord{
		record(1, mstorage.HistoryRecordUser, "", "杭州天气？"),
		record(2, mstorage.HistoryRecordTool, "get_weather", `{"city":"杭州","weather":"晴"}`),
		record(3, mstorage.HistoryRecordAssistant, "", "杭州明天晴。"),
	}

	result := AssembleContext("SYS", records, "那后天呢", DefaultContextPolicy())

	for _, msg := range result.Messages {
		if msg.Role == RoleTool {
			t.Fatalf("不应产生 role=tool 消息: %+v", result.Messages)
		}
	}
	assistant := result.Messages[2]
	if assistant.Role != RoleAssistant {
		t.Fatalf("第 3 条应为 assistant: %+v", result.Messages)
	}
	if !strings.Contains(assistant.Content, "杭州明天晴。") {
		t.Errorf("助手原话必须保留: %q", assistant.Content)
	}
	if !strings.Contains(assistant.Content, historyToolBlockOpen) ||
		!strings.Contains(assistant.Content, "get_weather") ||
		!strings.Contains(assistant.Content, `"weather":"晴"`) {
		t.Errorf("工具结果块应折叠进助手消息: %q", assistant.Content)
	}
}

// TestAssembleContextToolResultTruncated 单条工具结果超长时必须按字符截断(完整内容仍在历史文件里)。
func TestAssembleContextToolResultTruncated(t *testing.T) {
	long := strings.Repeat("测", 5000)
	records := []mstorage.MemoryRecord{
		record(1, mstorage.HistoryRecordUser, "", "问"),
		record(2, mstorage.HistoryRecordTool, "t", long),
		record(3, mstorage.HistoryRecordAssistant, "", "答"),
	}

	policy := DefaultContextPolicy()
	policy.ToolReplayMaxRunes = 100
	result := AssembleContext("SYS", records, "再问", policy)

	content := result.Messages[2].Content
	if strings.Contains(content, long) {
		t.Fatalf("超长工具结果应被截断")
	}
	if !strings.Contains(content, "…") {
		t.Errorf("截断应带省略标记: %q", content)
	}
	if strings.HasPrefix(content, "\xef\xbf\xbd") || !strings.Contains(content, "测") {
		t.Errorf("截断必须按字符边界进行(不得切出乱码): %q", content[:20])
	}
}

// TestAssembleContextLeftoverToolWithoutAssistant 被中断的轮次(只有工具结果、没有助手回复)
// 也必须保留工具产出,但不得生成孤立的 tool 消息。
func TestAssembleContextLeftoverToolWithoutAssistant(t *testing.T) {
	records := []mstorage.MemoryRecord{
		record(1, mstorage.HistoryRecordUser, "", "查一下"),
		record(2, mstorage.HistoryRecordTool, "get_weather", `{"weather":"雨"}`),
	}

	result := AssembleContext("SYS", records, "在吗", DefaultContextPolicy())

	if got := strings.Join(roles(result.Messages), ","); got != "system,user,assistant,user" {
		t.Fatalf("残留工具结果应作为助手消息回放: %s", got)
	}
	if !strings.Contains(result.Messages[2].Content, `"weather":"雨"`) {
		t.Errorf("工具产出不得丢失: %q", result.Messages[2].Content)
	}
}

// TestAssembleContextInterruptedAssistantReplayed 被中断的助手回复(部分内容)按普通助手消息回放。
func TestAssembleContextInterruptedAssistantReplayed(t *testing.T) {
	interrupted := record(2, mstorage.HistoryRecordAssistant, mstorage.MemoryRecordInterrupted, "回答到一半")
	interrupted.TimeText = "2026-09-13 01:00:00"
	records := []mstorage.MemoryRecord{
		record(1, mstorage.HistoryRecordUser, "", "问题"),
		interrupted,
	}

	result := AssembleContext("SYS", records, "继续", DefaultContextPolicy())
	if result.Messages[2].Role != RoleAssistant || result.Messages[2].Content != "回答到一半" {
		t.Fatalf("中断的部分回复应回放: %+v", result.Messages)
	}
}

// TestAssembleContextSummaryReplacesCoveredRecords 摘要记录折叠进 system,
// 且它 covers 的旧记录不再回放(避免同段历史既摘要又原文)。
func TestAssembleContextSummaryReplacesCoveredRecords(t *testing.T) {
	records := []mstorage.MemoryRecord{
		record(1, mstorage.HistoryRecordUser, "", "老问题一"),
		record(2, mstorage.HistoryRecordAssistant, "", "老回答一"),
		record(3, mstorage.HistoryRecordUser, "", "老问题二"),
		record(4, mstorage.HistoryRecordAssistant, "", "老回答二"),
		summaryRecord(100, 1, 4, "用户先问了两个老问题,已分别回答。"),
		record(101, mstorage.HistoryRecordUser, "", "新问题"),
		record(102, mstorage.HistoryRecordAssistant, "", "新回答"),
	}

	result := AssembleContext("SYS", records, "当前问题", DefaultContextPolicy())

	if !strings.Contains(result.Messages[0].Content, historySummaryTitle) ||
		!strings.Contains(result.Messages[0].Content, "已分别回答") {
		t.Fatalf("摘要应折叠进 system 消息: %q", result.Messages[0].Content)
	}
	joined := ""
	for _, msg := range result.Messages[1:] {
		joined += msg.Content
	}
	if strings.Contains(joined, "老问题一") || strings.Contains(joined, "老回答二") {
		t.Errorf("被摘要覆盖的记录不应再回放: %q", joined)
	}
	if !strings.Contains(joined, "新问题") || !strings.Contains(joined, "当前问题") {
		t.Errorf("摘要之后的新记录与本轮问题必须保留: %q", joined)
	}
	if got := strings.Join(roles(result.Messages), ","); got != "system,user,assistant,user" {
		t.Errorf("消息序列不正确: %s", got)
	}
}

// TestAssembleContextSkipsEmptyAssistant 空助手记录不回放(模型只输出思考内容时会写空回复),
// 否则会白占预算并让消息序列出现无信息量的空洞。
func TestAssembleContextSkipsEmptyAssistant(t *testing.T) {
	records := []mstorage.MemoryRecord{
		record(1, mstorage.HistoryRecordUser, "", "第一个问题"),
		record(2, mstorage.HistoryRecordAssistant, "", "   "), // 空回复
		record(3, mstorage.HistoryRecordUser, "", "第二个问题"),
		record(4, mstorage.HistoryRecordAssistant, "", "第二个回答"),
	}

	result := AssembleContext("SYS", records, "第三个问题", DefaultContextPolicy())
	if got := strings.Join(roles(result.Messages), ","); got != "system,user,user,assistant,user" {
		t.Fatalf("空助手记录不应回放: %s", got)
	}
}

// TestAssembleContextKeepsToolOutputWhenReplyEmpty 回复为空但已执行过工具时,
// 工具产出必须保留(单独成一条助手消息),不得随空助手记录一起被丢掉。
func TestAssembleContextKeepsToolOutputWhenReplyEmpty(t *testing.T) {
	records := []mstorage.MemoryRecord{
		record(1, mstorage.HistoryRecordUser, "", "杭州天气"),
		record(2, mstorage.HistoryRecordTool, "get_weather", `{"weather":"晴"}`),
		record(3, mstorage.HistoryRecordAssistant, "", ""), // 空回复
	}

	result := AssembleContext("SYS", records, "在吗", DefaultContextPolicy())
	if got := strings.Join(roles(result.Messages), ","); got != "system,user,assistant,user" {
		t.Fatalf("工具产出应保留为助手消息: %s", got)
	}
	if !strings.Contains(result.Messages[2].Content, `"weather":"晴"`) {
		t.Errorf("工具产出不得丢失: %q", result.Messages[2].Content)
	}
}

// TestAssembleContextBudgetTrimsOldestGroups 预算不足时从最老的历史开始裁剪,
// 保留区必须以 user 消息开头(不得出现 orphan 的 assistant/tool 开头)。
func TestAssembleContextBudgetTrimsOldestGroups(t *testing.T) {
	// 用较长的 ASCII 内容放大 token 估算,便于用较小的窗口触达裁剪
	long := strings.Repeat("history detail ", 40)
	var records []mstorage.MemoryRecord
	index := 1
	for i := 0; i < 6; i++ {
		records = append(records,
			record(index, mstorage.HistoryRecordUser, "", long),
			record(index+1, mstorage.HistoryRecordAssistant, "", long),
		)
		index += 2
	}

	policy := DefaultContextPolicy()
	policy.ContextWindow = 900
	policy.OutputReserve = 64     // 显式调小,使历史预算为正
	policy.ReverseTokenFloor = 64 // 系统提示预留(此处远小于 SYS 实际值,以实际值为准)
	result := AssembleContext("SYS", records, "本轮问题", policy)

	if !result.Trimmed || !result.NeedCompaction {
		t.Fatalf("预算不足时必须判定为已裁剪且需压缩: %+v", result)
	}
	if result.DroppedFrom != 1 {
		t.Errorf("裁剪应从最老记录开始(序号 1),实际 from=%d", result.DroppedFrom)
	}
	if result.DroppedTo < result.DroppedFrom || result.DroppedTo >= 12 {
		t.Errorf("被裁区间终点异常: %d", result.DroppedTo)
	}
	if len(result.Messages) >= len(records)+2 {
		t.Errorf("应确实裁掉了一些历史: messages=%d", len(result.Messages))
	}
	if result.Messages[1].Role != RoleUser {
		t.Errorf("保留区必须以 user 消息开头,实际 %q", result.Messages[1].Role)
	}
	// 最新的历史与本轮问题必须保留
	last := result.Messages[len(result.Messages)-1]
	if last.Content != "本轮问题" {
		t.Errorf("本轮用户消息必须位于末尾: %q", last.Content)
	}
	// 裁剪后应当落回窗口内(预算优先:不能把请求顶出窗口)
	if fitted := EstimateInputTokens(result.Messages); fitted > policy.ContextWindow {
		t.Errorf("裁剪后仍超窗: fitted=%d window=%d", fitted, policy.ContextWindow)
	}
}

// TestAssembleContextNoTrimWithoutWindow 未配置上下文窗口时不裁剪(保持阶段1行为)。
func TestAssembleContextNoTrimWithoutWindow(t *testing.T) {
	long := strings.Repeat("x", 40000)
	records := []mstorage.MemoryRecord{
		record(1, mstorage.HistoryRecordUser, "", long),
		record(2, mstorage.HistoryRecordAssistant, "", long),
	}

	result := AssembleContext("SYS", records, "问题", DefaultContextPolicy())
	if result.Trimmed || result.NeedCompaction {
		t.Fatalf("未配置窗口时不应裁剪: %+v", result)
	}
	if len(result.Messages) != 4 {
		t.Errorf("应完整回放: %d", len(result.Messages))
	}
}

// TestAssembleContextSystemFloorReservesPromptSpace reverse_token_floor 的语义是
// "为系统提示(AGENTS.md 等启动文档 + 历史摘要)预留的最小空间":
//   - 它约束的是"历史可以占多少",而不是"历史至少保留多少";
//   - 因此 floor 越大,窗口不够时**越早丢历史**,系统提示始终优先。
func TestAssembleContextSystemFloorReservesPromptSpace(t *testing.T) {
	long := strings.Repeat("y", 400)
	records := []mstorage.MemoryRecord{
		record(1, mstorage.HistoryRecordUser, "", long),
		record(2, mstorage.HistoryRecordAssistant, "", long),
		record(3, mstorage.HistoryRecordUser, "", long),
		record(4, mstorage.HistoryRecordAssistant, "", long),
	}

	// 预留额度超过整个窗口 → 历史预算为 0 → 不带历史(请求仍合法),系统提示优先
	tight := ContextPolicy{ContextWindow: 200, OutputReserve: 10, ReverseTokenFloor: 4000, ToolReplayMaxRunes: 100}
	result := AssembleContext("SYS", records, "问题", tight)

	if got := strings.Join(roles(result.Messages), ","); got != "system,user" {
		t.Fatalf("预留额度超出窗口时不应回放历史: %s", got)
	}
	if result.Messages[0].Content != "SYS" {
		t.Errorf("系统提示必须原样保留: %q", result.Messages[0].Content)
	}
	if !result.Trimmed || result.DroppedCount != len(records) {
		t.Errorf("全部历史应被判定为已裁: %+v", result)
	}

	// 预留额度越大,历史能占的越少(同样窗口下更早丢历史)。
	// 用短消息(约 25 tokens/条)让窗口内能放下多组,这样预算差异才会体现在回放量上。
	short := strings.Repeat("z", 100)
	var many []mstorage.MemoryRecord
	for i := 1; i <= 8; i++ {
		kind := mstorage.HistoryRecordUser
		if i%2 == 0 {
			kind = mstorage.HistoryRecordAssistant
		}
		many = append(many, record(i, kind, "", short))
	}

	small := ContextPolicy{ContextWindow: 400, OutputReserve: 10, ReverseTokenFloor: 0, ToolReplayMaxRunes: 100}
	large := ContextPolicy{ContextWindow: 400, OutputReserve: 10, ReverseTokenFloor: 200, ToolReplayMaxRunes: 100}

	keptSmall := len(AssembleContext("SYS", many, "问题", small).Messages)
	keptLarge := len(AssembleContext("SYS", many, "问题", large).Messages)
	if keptLarge >= keptSmall {
		t.Errorf("预留额度应减少历史回放量: floor=0 -> %d 条, floor=200 -> %d 条", keptSmall, keptLarge)
	}
}

// TestSystemReserveTokens 系统提示预留额度取 max(实际估算, floor)。
func TestSystemReserveTokens(t *testing.T) {
	system := llmproto.ChatMessage{Role: RoleSystem, Content: strings.Repeat("a", 400)} // 约 100 tokens

	if got := systemReserveTokens(ContextPolicy{}, system); got != EstimateInputTokens([]llmproto.ChatMessage{system}) {
		t.Errorf("未配置 floor 时应用实际估算: %d", got)
	}
	if got := systemReserveTokens(ContextPolicy{ReverseTokenFloor: 50}, system); got != EstimateInputTokens([]llmproto.ChatMessage{system}) {
		t.Errorf("floor 小于实际时应以实际为准: %d", got)
	}
	if got := systemReserveTokens(ContextPolicy{ReverseTokenFloor: 9999}, system); got != 9999 {
		t.Errorf("floor 大于实际时应以 floor 为准: %d", got)
	}
}

// TestFitMessagesWithoutWindow 未配置窗口时不改动消息序列。
func TestFitMessagesWithoutWindow(t *testing.T) {
	messages := []llmproto.ChatMessage{
		{Role: RoleSystem, Content: "SYS"},
		{Role: RoleUser, Content: "q1"},
		{Role: RoleAssistant, Content: "a1"},
	}
	got := FitMessages(dataobject.TLlm{}, messages, DefaultContextPolicy())
	if len(got) != len(messages) {
		t.Fatalf("未配置窗口不应裁剪: %d", len(got))
	}
}

// TestFitMessagesKeepsLastGroupAndPairs 工具循环中裁剪:保留末组(本轮用户消息 + 工具调用链),
// 且 assistant(tool_calls) 与其 tool 结果不得被切散。
func TestFitMessagesKeepsLastGroupAndPairs(t *testing.T) {
	long := strings.Repeat("z", 400)
	messages := []llmproto.ChatMessage{
		{Role: RoleSystem, Content: "SYS"},
	}
	for i := 0; i < 5; i++ { // 历史若干轮
		messages = append(messages,
			llmproto.ChatMessage{Role: RoleUser, Content: long},
			llmproto.ChatMessage{Role: RoleAssistant, Content: long},
		)
	}
	// 本轮:用户问题 + 工具调用 + 工具结果
	messages = append(messages,
		llmproto.ChatMessage{Role: RoleUser, Content: "本轮问题"},
		llmproto.ChatMessage{Role: RoleAssistant, ToolCalls: []llmproto.ToolCall{{ID: "c1"}}},
		llmproto.ChatMessage{Role: RoleTool, ToolCallID: "c1", Content: "工具结果"},
	)

	policy := DefaultContextPolicy()
	policy.ContextWindow = 700
	got := FitMessages(dataobject.TLlm{ContextWindow: 0}, messages, policy)

	if got[0].Role != RoleSystem {
		t.Fatalf("system 必须固定在首位: %+v", got)
	}
	if got[1].Role != RoleUser {
		t.Errorf("裁剪后必须以 user 消息开头(不得 orphan): %q", got[1].Role)
	}
	// 末组完整保留:assistant(tool_calls) 之后必须紧跟对应 tool 结果
	last := got[len(got)-1]
	if last.Role != RoleTool || last.ToolCallID != "c1" {
		t.Fatalf("末组的工具调用链必须完整保留: %+v", got[len(got)-3:])
	}
	if got[len(got)-2].Role != RoleAssistant || len(got[len(got)-2].ToolCalls) == 0 {
		t.Errorf("tool 结果之前必须保留其 assistant(tool_calls) 消息: %+v", got)
	}
	if len(got) >= len(messages) {
		t.Errorf("应确实裁掉了部分历史: got=%d all=%d", len(got), len(messages))
	}
}

// TestShouldCompact 压缩触发判定:compaction_mode=0(default) 与 1(safeguard) 都允许压缩,
// 但只有真的裁掉了历史才触发。
func TestShouldCompact(t *testing.T) {
	dropped := AssembleResult{NeedCompaction: true, DroppedFrom: 1, DroppedTo: 10}
	if !ShouldCompact(dataobject.TAgent{CompactionMode: dataobject.CompactionModeDefault}, dropped) {
		t.Error("default 模式应允许压缩")
	}
	if !ShouldCompact(dataobject.TAgent{CompactionMode: dataobject.CompactionModeSafeguard}, dropped) {
		t.Error("safeguard 模式应允许压缩")
	}
	if ShouldCompact(dataobject.TAgent{}, AssembleResult{}) {
		t.Error("未裁剪时不应触发压缩")
	}
}

// TestContextPolicyFor 策略推导:窗口与 reverse_token_floor 来自配置。
func TestContextPolicyFor(t *testing.T) {
	policy := GetContextPolicy(dataobject.TAgent{ReverseTokenFloor: 2048}, 32768)
	if policy.ContextWindow != 32768 || policy.ReverseTokenFloor != 2048 {
		t.Errorf("策略推导不正确: %+v", policy)
	}
	if policy.OutputReserve != DefaultOutputReserveTokens || policy.ToolReplayMaxRunes != DefaultToolReplayMaxRunes {
		t.Errorf("默认值应被补齐: %+v", policy)
	}
}
