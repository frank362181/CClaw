/**
* LLM token budgeter
*   防止预算熔断
*	对LLM的各个组成、context window的大小做预算
**/

package security

import (
	"encoding/json"
	"fmt"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/llmproto"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

// token 估算的假设(公开为包内常量:它同时被“输出上限是否覆盖字符上限”的契约测试引用)。
const (
	// CharsPerCJKToken CJK 字符每 token 的字符数(即 1 token ≈ 1.5 个汉字)。
	CharsPerCJKToken = 1.5
	// CharsPerAsciiToken 非 CJK 字符每 token 的字符数。
	CharsPerAsciiToken   = 4.0
	perMessageOverhead   = 4
	estimateSafetyFactor = 0.2
)

// MaintenanceBudgets 各维护动作本轮可发送的源文本预算
type MaintenanceBudgets struct {
	DistillBudget int
	CompactBudget int
	MergeBudget   int
}

// DistillPressure 蒸馏的"内容侧"压力。
//
// 全部由调用方从**已读回**的数据算出来(装配刚用过记录;水位只读一个 <=12KB 的 MEMORY.md),
// 不在请求路径上增加 IO 或全量扫描。
type DistillPressure struct {
	PendingRecords int  // 水位之后、尚未蒸馏的记录条数
	PendingChars   int  // 上述记录的正文总长(达到送入门槛 = 已够一轮)
	Closing        bool // 会话收尾(空闲超时/主动结束):必须把尾巴带走
}

func EstimateToolReserveTokens(agentId string, tools []llmproto.Tool) int {
	if len(tools) == 0 {
		return 0
	}

	payload, err := json.Marshal(tools)
	if err != nil {
		return 0
	}

	toolsReservedTokens := EstimateInputTokens([]llmproto.ChatMessage{{Role: hCommon.RoleSystem, Content: string(payload)}})
	return toolsReservedTokens
}

// PolicyWarnings 报告策略里"一看就会出问题"的配置(供调用方记日志;无问题时返回 nil)。
//
// 两类:
//   - 必然 BudgetExhausted:系统提示预留 + 输出预留 就已经吃掉窗口(含本轮消息更不够)
//     —— 此时历史一点都不回放,且压缩被禁用(NeedCompact 对 BudgetExhausted 返回 false);
//   - 疑似把"系统提示预留"当成历史旋钮用:预留超出实测系统提示 + 摘要上限 时,
//     真正应该调的是 MaxHistoryTokens。
//
// 阈值取"实测 + 摘要上限":预留的正当用途就是给摘要增长留余量,
// 超过这个量就不是"预留"了。
func PolicyWarnings(policy hCommon.ContextPolicy, systemTokens int) []string {
	var warnings []string

	if policy.ContextWindow <= 0 {
		return nil // 未配置窗口:不做任何预算推断,也就无可警告
	}

	summarySlack := CharsToTokens(hCommon.MaxSummaryTotalRunes) // 摘要总量上限的 token 当量
	if policy.SystemPromptReserveTokens > systemTokens+summarySlack {
		warnings = append(warnings, fmt.Sprintf(
			"SystemPromptReserveTokens=%d exceeds the measured system prompt (%d tokens) + summary cap (%d tokens); "+
				"if the intent is \"replay less history\", use MaxHistoryTokens instead",
			policy.SystemPromptReserveTokens, systemTokens, summarySlack))
	}

	if headroom := policy.SystemPromptReserveTokens + policy.OutputReserve + policy.ToolsReserveTokens; headroom >= policy.ContextWindow {
		warnings = append(warnings, fmt.Sprintf(
			"system reserve (%d) + output reserve (%d) + tools reserve (%d) >= context window (%d): "+
				"history will never be replayed and compaction is disabled",
			policy.SystemPromptReserveTokens, policy.OutputReserve, policy.ToolsReserveTokens, policy.ContextWindow))
	}

	// 维护调用(蒸馏/压缩/合并)同样要装进窗口:装不下时它们会被**整批跳过**,
	if avail := CalcMaintenanceAvailableTokens(policy.ContextWindow, hCommon.SummaryMaxTokens, ""); avail <= 0 {
		warnings = append(warnings, fmt.Sprintf(
			"context window (%d) minus the maintenance output budget (%d tokens) leaves no room for a maintenance call: "+
				"distillation/compaction/merge will be skipped (use a larger window or lower SummaryMaxTokens)",
			policy.ContextWindow, hCommon.SummaryMaxTokens))
	}

	return warnings
}

// CalcBoostrapBudget 计算启动文档注入的总预算(**rune**)。
func CalcBoostrapBudget(configured int) int {
	switch {
	case configured > 0:
		return configured
	case configured < 0:
		return 0
	default:
		return hCommon.MaxBootstrapTotalChars
	}
}

// CalcMaintenanceBudgets 按模型窗口推导各动作的源文本预算
func CalcMaintenanceBudgets(windowTokens int,
	distillSystemPrompt string,
	compactionSystemPrompt string,
	compactionMergeSystemPrompt string) MaintenanceBudgets {
	return MaintenanceBudgets{
		DistillBudget: calcMaintenanceInputBudgetRunes(windowTokens, hCommon.SummaryMaxTokens, distillSystemPrompt, hCommon.DistillMaxInputChars),
		CompactBudget: calcMaintenanceInputBudgetRunes(windowTokens, hCommon.SummaryMaxTokens, compactionSystemPrompt, hCommon.MaxCompactionInputChars),
		MergeBudget:   calcMaintenanceInputBudgetRunes(windowTokens, hCommon.SummaryMaxTokens, compactionMergeSystemPrompt, hCommon.MaxCompactionInputChars),
	}
}

// CalcMaintenanceAvailableTokens 报告维护调用的"可用 token 额度"(窗口 - 输出 - 系统提示估量)。
// <=0 表示这个模型窗口装不下任何维护调用。供 PolicyWarnings 输出一条可操作的配置告警。
func CalcMaintenanceAvailableTokens(windowTokens, outputTokens int, systemPrompt string) int {
	if windowTokens <= 0 {
		return 0
	}
	systemTokens := EstimateInputTokens([]llmproto.ChatMessage{{Role: hCommon.RoleSystem, Content: systemPrompt}})
	return windowTokens - outputTokens - systemTokens
}

// CalcMaintenanceOutputTokens 计算一次维护调用应发送的 max_tokens。
//
// 返回 <=0 表示**装不下,调用方应跳过**(而不是发一个必然失败或必然被截断的请求):
// 维护调用的输入同样占窗口,只按 SummaryMaxTokens 硬发会在小窗模型上直接越界。
//   - windowTokens <= 0(未配置窗口):沿用 maxTokens(与"窗口未知就不裁剪"同一口径);
//   - 剩余额度不足 MaintenanceMinOutputTokens:返回 0(残文没有写入价值);
//   - 否则取 min(剩余额度, maxTokens)。
func CalcMaintenanceOutputTokens(windowTokens, maxTokens int, systemPrompt, userPrompt string) int {
	if windowTokens <= 0 {
		return maxTokens
	}

	room := windowTokens - EstimateInputTokens([]llmproto.ChatMessage{
		{Role: hCommon.RoleSystem, Content: systemPrompt},
		{Role: hCommon.RoleUser, Content: userPrompt},
	})
	if room < hCommon.MaintenanceMinOutputTokens {
		return 0
	}
	if room < maxTokens {
		return room
	}
	return maxTokens
}

// Budget 返回"除历史之外可以动的输入额度":窗口扣除输出预留、系统提示预留、
// 以及工具 schema 预留(工具定义随请求单独发送,不占 messages,但同样占窗口)
func Budget(policy hCommon.ContextPolicy, msg llmproto.ChatMessage) int {
	reserved := CalcSystemReservedTokens(policy, msg)
	budget := policy.ContextWindow - policy.OutputReserve - reserved - policy.ToolsReserveTokens

	return budget
}

// DynamicMaxTokens 计算应发送的 max_tokens。由 context_window 与供应商输出上限共同约束:
//   - 取值为 min(context_window - 估算输入, llmProvider.MaxTokens);
//   - 剩余空间 ≤ 0 时取 1(此时请求大概率仍会被供应商拒绝,但保持确定性);
//   - 未配置 ContextWindow 时返回 0(不发送 max_tokens,由供应商用其默认值)。
//
// 修复背景:早期版本仅按 context_window 推导(如 1M 窗口 + 小输入 → max_tokens ≈ 1M),
func DynamicMaxTokens(llmProvider dataobject.TLlm, messages []llmproto.ChatMessage) int {
	if llmProvider.ContextWindow == 0 {
		return 0 // 未配置返回 0 表示应用供应商的默认值
	}

	budget := int(llmProvider.ContextWindow) - EstimateInputTokens(messages)
	if budget < 1 {
		budget = 1
	}
	if budget > llmProvider.MaxTokens {
		//确保不会超过LLM供应商单次输出的上限
		budget = llmProvider.MaxTokens
	}
	return budget
}

// EstimateInputTokens 粗略估算 messages 的输入 token 数(非精确,仅用于动态裁剪)。
// 估算规则:
//   - 中文/日文/韩文(CJK)字符按 ~1.5 字符/token;
//   - 其他字符(如英文/数字/标点)按 ~4 字符/token;
//   - 每条消息附加 ~4 token 的角色/结构开销;
//   - 结果上浮 20% 作为安全余量,避免估算偏低导致"输入 + max_tokens"仍越界。
func EstimateInputTokens(messages []llmproto.ChatMessage) int {
	total := 0
	for _, m := range messages {
		cjk, other := 0, 0
		for _, r := range m.Content {
			if pCommon.IsCJK(r) {
				cjk++
			} else {
				other++
			}
		}
		total += perMessageOverhead + int(float64(cjk)/CharsPerCJKToken) + int(float64(other)/CharsPerAsciiToken)
	}
	return int(float64(total) * (1 + estimateSafetyFactor))
}

// SummaryOverBudget 生效摘要的**注入体积**是否已超上限。
// 体积口径统一在 hCommon.SummariesRunes(== SummaryTextOf(records) 的 rune 数),
// 与 SummaryTextOverBudget 对同一段文本给出同一结论。
func SummaryOverBudget(records []hCommon.MemoryRecord) bool {
	return hCommon.SummariesRunes(records) > hCommon.MaxSummaryTotalRunes
}

// SummaryTextOverBudget 将要注入 system 的摘要正文是否已超总量上限。
//
// 与 SummaryOverBudget(records) **同源**:后者按记录渲染出同一段文本再比
// (hCommon.SummaryTextOf),因此"触发判定(注入文本)"与"动作门(文件记录)"得到同一个结论。
func SummaryTextOverBudget(summary string) bool {
	return hCommon.TextRunes(summary) > hCommon.MaxSummaryTotalRunes
}

// NeedCompact 判断是否压缩(与 compaction_mode 无关)。
//
// 三个维度:
//  1. **正确性主干**:确实有记录掉出窗口(DroppedFrom>0)且条数超过门槛 ——
//     只有掉出窗口的记录才需要被摘要代表。提前压缩(历史没超预算就压)反而**有害**:
//     那会用摘要替换还能回放的原文,白丢信息而预算收益为零。
//  2. **收益**:被裁区间的体积达到 MinCompactionSrcRunes —— 体积小于摘要长度下限时
//     压完不会更小,只白烧一次 LLM 调用(精确判定仍在 Compact 内部按选中的那一段做)。
//  3. **预算**:摘要总量已超上限时**不压**,先让合并把摘要整理下来 ——
//     否则 system 单调增长 → 历史预算更小 → 裁更多 → 再压,形成正反馈。
//     被裁记录一直留在文件里,下一轮仍是同一批,不会漏(最多延迟一轮)。
//
// 历史预算为 0 时永不触发:压缩改变不了 system 预留,问题在配置。
func NeedCompact(result hCommon.AssembleResult, droppedRunes int) bool {
	if result.BudgetExhausted {
		return false //budget=0,就没必要压缩了
	}
	if result.DroppedFrom <= 0 || result.DroppedRecordCount <= hCommon.CompactionMinRecords {
		return false
	}
	if droppedRunes < hCommon.MinCompactionSrcRunes {
		return false
	}

	return !SummaryTextOverBudget(result.Summary)
}

func NewDistillPressure(closing bool) DistillPressure {
	return DistillPressure{
		Closing: closing,
	}
}

// NeedDistillPressure 报告"本轮的蒸馏压力是否会影响判定"。
//
// 压力只在内容检查节拍上参与(见 NeedDistill):其余轮次要么走节拍、要么是收尾,
// 都不看压力。调用方据此省掉一次 MEMORY.md 读取 + 扫描(实测约 35-80µs)。
func NeedDistillPressure(turns int) bool {
	return turns > 0 && turns%hCommon.DistillCheckIntervalTurns == 0
}

// NeedDistill 判断是否蒸馏。
//
// 节拍只是**速率上限**;真正决定"该不该蒸"的是内容:
//   - **收尾**:会话收尾必须蒸一次。节拍是每 DistillIntervalTurns 轮一次,而会话在哪一轮停下是随机的,
//     最后不足一个节拍窗口的那几轮(真实数据:22 轮会话只在第 10/20 轮蒸过,第 21-22 轮永远没蒸)
//     不在这里带走就再也进不了长期记忆。
//   - **内容**:水位之后攒够一轮送入门槛就提前蒸。这条同时涵盖"上一轮没排空" ——
//     积压一旦攒到一个批次就会再次触发,不需要额外的会话状态。
//   - **首次**:恰好踩在最少轮次上也能蒸(否则 DistillMinTurns 会被 10 的倍数完全吃掉)。
//   - **节拍**:兜底,保证"内容稀疏但确实有东西"的会话也会定期蒸。
//
// 内容触发按 DistillCheckIntervalTurns 轮才生效一次:蒸馏持续失败时不应每轮重试同一份输入。
func NeedDistill(turns int, p DistillPressure) bool {
	if turns <= 0 {
		return false // 本轮没有落库(序号未推进):没有"轮次"可言,交给节拍以外的信号
	}
	if p.Closing {
		return true
	}
	if p.PendingChars >= hCommon.DistillMaxInputChars && turns%hCommon.DistillCheckIntervalTurns == 0 {
		return true
	}
	if turns == hCommon.DistillMinTurns {
		return true
	}

	return turns > hCommon.DistillMinTurns && turns%hCommon.DistillIntervalTurns == 0
}

// NeedMerge 判断是否合并摘要:摘要总量超上限,且预算没有耗尽。
//
// 预算耗尽时不做合并 —— 摘要再小也装不下历史,问题在配置(与压缩被禁同一个理由)。
func NeedMerge(result hCommon.AssembleResult) bool {
	return !result.BudgetExhausted && SummaryTextOverBudget(result.Summary)
}

// CharsToTokens 把 rune 数按估算规则换算为 token(最坏情况按 CJK 计)。
//
// 存在意义:启动文档的上限用 rune 表达(20K/4K/32K/60K),而窗口/预留/历史上限用 token 表达,
// 两套单位不换算就无法比较 —— 实测 61440 rune ≈ 41k token,是 SystemPromptReserveTokens
// 默认量级的 5 倍,很容易被误解成"系统提示最多 8k token"。
func CharsToTokens(runes int) int {
	if runes <= 0 {
		return 0
	}

	return int(float64(runes) / CharsPerCJKToken)
}

// TokensToChars 把 token 额度换算回可发送的字符数,**按最坏情况**(CJK 1.5 rune/token)。
//
// 方向很关键:预算宁可少发也不能超窗 —— 若按 ASCII 的 4 rune/token 换算,
// 中文内容会直接把请求顶出窗口。
func TokensToChars(tokens int) int {
	if tokens <= 0 {
		return 0
	}

	return int(float64(tokens) * CharsPerCJKToken)
}

// CalcSystemReservedTokens 计算系统提示应占用的 token 额度。
//
// 取 max(系统提示实际估量, policy.SystemPromptReserveTokens):
//   - 预留额度的语义是"为启动文档 + 折叠进 system 的摘要留出的最小空间",
//     即它约束的是**历史可以占多少**,而不是"历史至少保留多少";
//   - 系统提示实际更大时以实际为准(不能因为预留小就超窗);
//   - 它只在"大于实际估量"时才起作用 —— 启动文档大小已由估量覆盖,
//     这个额度真正兜的是摘要增长(上限 MaxSummaryTotalRunes)。
func CalcSystemReservedTokens(policy hCommon.ContextPolicy, sysMsg llmproto.ChatMessage) int {
	reversed := EstimateInputTokens([]llmproto.ChatMessage{sysMsg})
	if policy.SystemPromptReserveTokens > reversed {
		return policy.SystemPromptReserveTokens
	}

	return reversed
}

// ======================== private functions & methods ===========================

// calcMaintenanceInputBudgetRunes 计算维护调用(蒸馏/压缩/合并)可发送的**源文本**预算(rune)
func calcMaintenanceInputBudgetRunes(windowTokens, outputTokens int, systemPrompt string, hardCapRunes int) int {
	if hardCapRunes <= 0 {
		return 0
	}
	if windowTokens <= 0 {
		return hardCapRunes
	}

	systemTokens := EstimateInputTokens([]llmproto.ChatMessage{{Role: hCommon.RoleSystem, Content: systemPrompt}})
	avail := windowTokens - outputTokens - systemTokens
	if avail < 0 {
		avail = 0
	}
	if windowRunes := TokensToChars(avail); windowRunes < hardCapRunes {
		return windowRunes
	}
	return hardCapRunes
}
