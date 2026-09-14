/**
* 上下文读回装配
*
* 职责:把"系统提示 + 历史记忆(文件记录) + 本轮用户消息"装配成一次 LLM 调用所需的完整消息序列,
* 并在模型上下文窗口内做预算裁剪。它是"读回"的唯一入口,与 mstorage 的写入端一一对应。
*
* 装配形态(与写入端记录类型的映射):
*
*   user 记录      → role=user
*   tool 记录      → 折叠进"同轮 assistant 消息"的工具结果块(见下),不单独成消息
*   assistant 记录 → role=assistant(被中断的记录同样按普通助手消息回放)
*   summary 记录   → 折叠进 system 消息的"历史摘要"段,并使其 covers 区间内的旧记录不再回放
*
* 为什么工具结果要"折叠"而不是重建 role=tool 消息:
*   历史文件不保存 tool_call_id 与原始 tool_calls 数组,若凭猜测合成配对关系,
*   一旦与协议要求不一致(缺 tool_call_id、配对错位),上游会直接报协议错误。
*   折叠成文本块既保住了工具产出的信息,又永远不会破坏协议契约。
*
* 预算裁剪(性能与稳定性):
*   - 只在"组边界"(以 user 消息为起点)上裁剪,绝不切散 assistant/tool 配对;
*   - t_agent.reverse_token_floor 是**为系统提示(AGENTS.md 等启动文档 + 历史摘要)预留的最小额度**,
*     它约束的是"历史可以占多少"而非"历史至少保留多少":预留额度越大,历史回放得越少;
*   - 历史预算是硬上限:算不下就不带历史(请求仍然合法:system + 本轮用户消息),
*     不会为了多带历史把请求顶出模型窗口;
*   - 被裁掉的记录区间会回传给调用方,作为"需要压缩"的判据(压缩由 mstorage 落盘为新摘要记录)。
**/

package ctxmgr

import (
	"fmt"
	"strings"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/llmproto"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

// 装配默认参数
const (
	// DefaultToolReplayMaxRunes 单条工具结果回放的最大字符数(控制历史体积;完整内容仍在文件里)
	DefaultToolReplayMaxRunes = 1200
	// DefaultOutputReserveTokens 为模型输出预留的 token 数
	DefaultOutputReserveTokens = 1024
)

// 工具结果块标记:既是给模型的语义分隔,也是"这段不是助手原话"的明确提示。
const (
	historyToolBlockOpen  = "<工具结果>"
	historyToolBlockClose = "</工具结果>"
	historySummaryTitle   = "## 历史摘要"
)

// ContextPolicy 上下文装配策略(来源:t_agent 与 t_llm 配置)。
type ContextPolicy struct {
	ContextWindow      int // 模型上下文窗口(tokens);<=0 表示未知,不做裁剪(保持阶段1行为)
	OutputReserve      int // 为输出预留的 tokens;<=0 取默认
	ReverseTokenFloor  int // 为系统提示(AGENTS.md 等启动文档 + 历史摘要)预留的最小 tokens(t_agent.reverse_token_floor)
	ToolReplayMaxRunes int // 单条工具结果回放的最大字符数;<=0 取默认
}

// DefaultContextPolicy 返回默认策略(未配置任何压缩参数时的安全值)。
func DefaultContextPolicy() ContextPolicy {
	return ContextPolicy{
		OutputReserve:      DefaultOutputReserveTokens,
		ToolReplayMaxRunes: DefaultToolReplayMaxRunes,
	}
}

// GetContextPolicy 按 Agent 与模型配置推导装配策略。
// compaction_mode 与 reverse_token_floor 是 t_agent 上为"压缩"预留的配置项;
// reverse_token_floor 是为系统提示(AGENTS.md 等启动文档、以及折叠进 system 的历史摘要)
// 预留的**最小** token 额度 —— 它保护的是"规则文档不被历史挤掉",因此在这里读取。
func GetContextPolicy(agent dataobject.TAgent, contextWindow int) ContextPolicy {
	policy := DefaultContextPolicy()
	policy.ContextWindow = contextWindow
	policy.ReverseTokenFloor = agent.ReverseTokenFloor

	return policy
}

// AssembleResult 装配结果。
type AssembleResult struct {
	Messages []llmproto.ChatMessage // 完整消息序列(system + 历史 + 本轮用户)
	Summary  string                 // 折叠进 system 的历史摘要文本(为空表示无摘要)

	Trimmed        bool // 是否因预算裁剪掉了历史
	DroppedFrom    int  // 被裁掉的记录序号区间起点(闭区间;0 表示无)
	DroppedTo      int  // 被裁掉的记录序号区间终点(闭区间;0 表示无)
	DroppedCount   int  // 被裁掉的记录条数
	NeedCompaction bool // 是否需要压缩(裁剪已发生 → 应当把被裁区间压缩为摘要)
}

// historyItem 一条历史消息及其来源记录序号(用于裁剪后报告被裁区间)。
type historyItem struct {
	index int // 来源记录序号;多条工具记录合成时为其中最大序号
	msg   llmproto.ChatMessage
}

// AssembleContext 装配完整上下文。
//   - systemPrompt:   已构建好的系统提示(含 AGENTS/SOUL/... 各段)
//   - records:        历史文件读回的记录(按序号升序)
//   - currentUserMsg: 本轮用户消息(必须由调用方传入,避免与历史中的最后一轮重复)
//   - policy:         装配策略
//
// 返回的消息序列永远以 system 开头、以本轮用户消息结尾。
func AssembleContext(systemPrompt string, records []hCommon.MemoryRecord, currentUserMsg string, policy ContextPolicy) AssembleResult {
	policy = normalizePolicy(policy)

	summaryText, active := splitSummaryAndActive(records)
	items := buildHistoryItems(active, policy.ToolReplayMaxRunes)

	system := composeSystemMessage(systemPrompt, summaryText)
	current := llmproto.ChatMessage{Role: hCommon.RoleUser, Content: currentUserMsg}

	result := AssembleResult{Summary: summaryText}

	// 历史预算 = 窗口 - 输出预留 - 系统提示预留 - 本轮用户消息
	// 系统提示预留取 max(实际估算, reverse_token_floor):即便系统提示很小(或将来变短),
	// 也为 AGENTS.md 等规则文档保留 floor 的额度;系统提示实际更大时以实际为准。
	//
	// 窗口未知(ContextWindow<=0)时不裁剪,保持阶段1行为(不做任何预算推断)。
	var kept, dropped []historyItem
	if policy.ContextWindow <= 0 {
		kept = items
	} else {
		budget := policy.ContextWindow - policy.OutputReserve -
			systemReserveTokens(policy, system) - EstimateInputTokens([]llmproto.ChatMessage{current})
		if budget < 0 {
			budget = 0 // 窗口不够:本轮不带历史(而不是强行塞入历史把请求顶出窗口)
		}
		kept, dropped = trimHistoryItems(items, budget)
	}
	if len(dropped) > 0 {
		result.Trimmed = true
		result.NeedCompaction = true
		result.DroppedCount = len(dropped)
		result.DroppedFrom = dropped[0].index
		result.DroppedTo = dropped[len(dropped)-1].index
	}

	messages := make([]llmproto.ChatMessage, 0, len(kept)+2)
	messages = append(messages, system)
	for _, item := range kept {
		messages = append(messages, item.msg)
	}
	messages = append(messages, current)

	result.Messages = messages
	return result
}

// FitMessages 在上下文窗口内裁剪一段已有消息序列(工具循环内每轮调用前使用)。
//
// 与 AssembleContext 的区别:它作用于"已经装好的消息"——工具循环会把工具调用与结果
// 追加到尾部,历史会被逐步挤占,因此在每次请求上游之前都要重新贴合窗口。
//
// 裁剪规则:
//   - 首条 system 消息固定保留(它承载系统提示与历史摘要);
//   - 其余消息按"组"裁剪,组以 user 消息为起点(assistant/tool 配对因此在同一组内,不会被切散);
//   - 末组(本轮用户消息 + 其工具调用链)永远保留 —— 它属于本轮,丢了就没法继续;
//   - 历史预算同样要扣除为系统提示预留的 reverse_token_floor(见 systemReserveTokens)。
func FitMessages(llmProvider dataobject.TLlm, messages []llmproto.ChatMessage, policy ContextPolicy) []llmproto.ChatMessage {
	if len(messages) <= 1 {
		return messages
	}

	policy = normalizePolicy(policy)
	if policy.ContextWindow <= 0 { // 未配置窗口:不做裁剪,保持阶段1行为
		return messages
	}

	groups := groupMessages(messages[1:])
	if len(groups) <= 1 {
		return messages
	}

	budget := policy.ContextWindow - policy.OutputReserve - systemReserveTokens(policy, messages[0])
	if budget < 0 {
		budget = 0
	}

	// 从末组向前累加,直到超出预算(末组永远保留)
	total := 0
	keepFrom := len(groups) - 1
	for i := len(groups) - 1; i >= 0; i-- {
		groupTokens := EstimateInputTokens(groups[i])
		if i < len(groups)-1 && total+groupTokens > budget {
			break
		}
		total += groupTokens
		keepFrom = i
	}

	if keepFrom == 0 {
		return messages // 无需裁剪
	}

	fitted := make([]llmproto.ChatMessage, 0, 1+total+1)
	fitted = append(fitted, messages[0])
	for _, group := range groups[keepFrom:] {
		fitted = append(fitted, group...)
	}
	return fitted
}

// ShouldCompact 判断装配结果是否触发压缩 —— **与模式无关**:default/safeguard 都触发,
// 如何压缩交给compaction mode来决定
func ShouldCompact(agent dataobject.TAgent, result AssembleResult) bool {
	return result.NeedCompaction && result.DroppedFrom > 0 && result.DroppedTo >= result.DroppedFrom
}

// ============================== private functions & methods ==============================

// normalizePolicy 补齐策略默认值。
func normalizePolicy(policy ContextPolicy) ContextPolicy {
	if policy.OutputReserve <= 0 {
		policy.OutputReserve = DefaultOutputReserveTokens
	}
	if policy.ToolReplayMaxRunes <= 0 {
		policy.ToolReplayMaxRunes = DefaultToolReplayMaxRunes
	}
	if policy.ReverseTokenFloor < 0 {
		policy.ReverseTokenFloor = 0
	}
	return policy
}

// composeSystemMessage 组装 system 消息:系统提示 + 历史摘要(摘要追加在系统提示之后)。
//
// 摘要刻意不单独成一条 system 消息:部分供应商对"非首条 system"支持不一致,
// 折进同一条 system 消息可以完全规避该类兼容问题。
func composeSystemMessage(systemPrompt, summaryText string) llmproto.ChatMessage {
	content := strings.TrimSpace(systemPrompt)
	if summary := strings.TrimSpace(summaryText); summary != "" {
		content = strings.TrimSpace(content + "\n\n" + historySummaryTitle + "\n" + summary)
	}
	return llmproto.ChatMessage{Role: hCommon.RoleSystem, Content: content}
}

// splitSummaryAndActive 拆出"摘要文本"与"仍需回放的记录"。
//
// 摘要记录(covers: from-to)取代了区间内的原始记录:被取代的记录不再回放,
// 否则同一段历史会既以摘要、又以原文出现,既浪费预算也干扰模型判断。
func splitSummaryAndActive(records []hCommon.MemoryRecord) (summaryText string, active []hCommon.MemoryRecord) {
	var summaries []hCommon.MemoryRecord
	covered := make(map[int]bool)

	for _, rec := range records {
		if rec.IsSummary() {
			summaries = append(summaries, rec)
			for i := rec.CoversFrom; i > 0 && i <= rec.CoversTo; i++ {
				covered[i] = true
			}
			continue
		}
		active = append(active, rec)
	}

	if len(summaries) == 0 {
		return "", filterCovered(active, covered)
	}

	// 多条摘要按序号升序拼接(序号即时间顺序),
	// 后一条摘要若覆盖了前一条,则前一条本身也被取代,不再回放。
	var texts []string
	for i, rec := range summaries {
		superseded := false
		for _, later := range summaries[i+1:] {
			if later.Covers(rec.Index) {
				superseded = true
				break
			}
		}
		if superseded {
			continue
		}
		if content := strings.TrimSpace(rec.Content); content != "" {
			texts = append(texts, content)
		}
	}
	return strings.Join(texts, "\n\n"), filterCovered(active, covered)
}

// filterCovered 过滤掉已被摘要取代的记录。
func filterCovered(records []hCommon.MemoryRecord, covered map[int]bool) []hCommon.MemoryRecord {
	if len(covered) == 0 {
		return records
	}
	kept := make([]hCommon.MemoryRecord, 0, len(records))
	for _, rec := range records {
		if !covered[rec.Index] {
			kept = append(kept, rec)
		}
	}
	return kept
}

// buildHistoryItems 把记录映射为历史消息(工具记录折叠进同轮助手消息)。
func buildHistoryItems(records []hCommon.MemoryRecord, toolMaxRunes int) []historyItem {
	items := make([]historyItem, 0, len(records))
	var pending []hCommon.MemoryRecord

	flush := func(target *historyItem, anchor int) {
		if len(pending) == 0 {
			return
		}
		block := renderToolBlock(pending, toolMaxRunes)
		if target != nil {
			target.msg.Content = strings.TrimSpace(target.msg.Content + "\n\n" + block)
		} else {
			// 该轮没有助手回复(如被中断):工具结果仍要保留,单独作为一条助手消息回放
			items = append(items, historyItem{index: anchor, msg: llmproto.ChatMessage{Role: hCommon.RoleAssistant, Content: block}})
		}
		pending = nil
	}

	for _, rec := range records {
		switch rec.RecordType {
		case hCommon.HistoryRecordTypeUser: //mstorage.HistoryRecordUser:
			// 上一条用户消息之前残留的工具记录(异常结构)不再挂到新问题上
			flush(nil, rec.Index)
			if strings.TrimSpace(rec.Content) == "" {
				continue // 空提问无信息量(异常数据),不回放
			}
			items = append(items, historyItem{index: rec.Index, msg: llmproto.ChatMessage{Role: hCommon.RoleUser, Content: rec.Content}})
		case hCommon.HistoryRecordTypeTool: //mstorage.HistoryRecordTool:
			pending = append(pending, rec)
		case hCommon.HistoryRecordTypeAssistant: //mstorage.HistoryRecordAssistant:
			// 空助手消息(模型只输出思考内容、或内容被安全策略拦下)不回放:
			// 它不携带任何信息,却会占用预算并干扰模型对消息序列的判断。
			// 若本轮还有工具结果,下面 flush 会单独成一条助手消息,工具产出不会丢。
			if strings.TrimSpace(rec.Content) == "" {
				continue
			}
			item := historyItem{index: rec.Index, msg: llmproto.ChatMessage{Role: hCommon.RoleAssistant, Content: rec.Content}}
			flush(&item, rec.Index)
			items = append(items, item)
		}
	}
	if len(pending) > 0 {
		flush(nil, pending[len(pending)-1].Index)
	}
	return items
}

// renderToolBlock 把同轮工具结果渲染为一个文本块(控制单条长度,完整内容仍在历史文件里)。
func renderToolBlock(tools []hCommon.MemoryRecord, maxRunes int) string {
	var sb strings.Builder
	sb.WriteString(historyToolBlockOpen)
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Title)
		if name == "" {
			name = "tool"
		}
		_, _ = fmt.Fprintf(&sb, "\n- %s: %s", name, pCommon.TruncateRunes(strings.TrimSpace(tool.Content), maxRunes, hCommon.TruncatedMarker))
	}
	sb.WriteString("\n")
	sb.WriteString(historyToolBlockClose)
	return sb.String()
}

// systemReserveTokens 计算系统提示应占用的 token 额度。
//
// 取 max(系统提示实际估算, policy.ReverseTokenFloor):
//   - reverse_token_floor 的语义是"为 AGENTS.md 等启动文档预留的最小空间",
//     即它约束的是**历史可以占多少**,而不是"历史至少保留多少";
//   - 系统提示实际更大时以实际为准(不能因为 floor 小就超窗)。
//
// 历史摘要会被折叠进 system 消息,因此这个额度同时也保护了摘要不被历史挤掉。
func systemReserveTokens(policy ContextPolicy, system llmproto.ChatMessage) int {
	reserve := EstimateInputTokens([]llmproto.ChatMessage{system})
	if policy.ReverseTokenFloor > reserve {
		return policy.ReverseTokenFloor
	}
	return reserve
}

// trimHistoryItems 在预算内保留尽可能新的历史(从尾部向前累加),返回保留与丢弃的两部分。
//
// 裁剪点必须落在"组边界"(user 消息)上:否则历史会以一条 orphan 的 assistant/tool 消息开头,
// 既容易让模型误判上下文,也可能与协议对消息顺序的要求冲突。
// budgetTokens <= 0 时返回"不带历史"(请求仍然合法:system + 本轮用户消息)。
func trimHistoryItems(items []historyItem, budgetTokens int) (kept, dropped []historyItem) {
	if len(items) == 0 {
		return nil, nil
	}
	if budgetTokens <= 0 {
		return nil, items // 窗口不够:不带历史(而不是强行塞入把请求顶出窗口)
	}

	cut := 0 // 保留 items[cut:]
	total := 0
	for i := len(items) - 1; i >= 0; i-- {
		itemTokens := EstimateInputTokens([]llmproto.ChatMessage{items[i].msg})
		if i < len(items)-1 && total+itemTokens > budgetTokens {
			break
		}
		total += itemTokens
		cut = i
	}

	// 对齐到组边界:保留区必须以 user 消息开头
	for cut < len(items) && items[cut].msg.Role != hCommon.RoleUser {
		cut++
	}
	if cut >= len(items) { // 极端情况:预算连一条消息都放不下 → 至少保留最新一条 user 消息
		for i := len(items) - 1; i >= 0; i-- {
			if items[i].msg.Role == hCommon.RoleUser {
				cut = i
				break
			}
		}
	}
	if cut <= 0 {
		return items, nil
	}
	return items[cut:], items[:cut]
}

// groupMessages 把消息按"组"切分:组以 user 消息为起点,后续 assistant/tool 消息归属该组。
// 目的:裁剪时以组为单位,保证 assistant(tool_calls) 与其 tool 结果不被切散。
func groupMessages(messages []llmproto.ChatMessage) [][]llmproto.ChatMessage {
	var groups [][]llmproto.ChatMessage
	for _, msg := range messages {
		if len(groups) == 0 || msg.Role == hCommon.RoleUser {
			groups = append(groups, []llmproto.ChatMessage{msg})
			continue
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], msg)
	}
	return groups
}
