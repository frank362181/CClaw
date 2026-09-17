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
*   - `SystemPromptReserveTokens`(DB 列名仍是 `t_agent.reverse_token_floor`)是**为系统提示
*     (7 个启动文档 + 折叠进 system 的历史摘要)预留的最小额度**:它约束"历史可以占多少",
*     而不是"历史至少保留多少"。注意它只会在**大于实测系统提示估量**时生效
*     —— 启动文档本身的大小已由估量覆盖,这个额度是给摘要增长留的余量,
*     因此取值应与 `MaxSummaryTotalRunes` 同量级,而不是与窗口同量级;
*   - `MaxHistoryTokens` 是**历史回放上限**:想"多早就开始压缩历史"就调它 ——
*     它是独立旋钮,不影响工具循环(FitMessages 不会被它限制);
*   - 历史预算是硬上限:算不下就不带历史(请求仍然合法:system + 本轮用户消息),
*     不会为了多带历史把请求顶出模型窗口;
*   - 被裁掉的记录区间会回传给调用方,作为"需要压缩"的判据(压缩由 mstorage 落盘为新摘要记录)。
**/

package ctxmgr

import (
	"fmt"
	"strings"

	"my.favorite/cclawservice/harness"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/security"
	"my.favorite/cclawservice/llmproto"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

// 工具结果块标记:既是给模型的语义分隔,也是"这段不是助手原话"的明确提示。
const (
	historyToolBlockOpen  = "<工具结果>"
	historyToolBlockClose = "</工具结果>"
	historySummaryTitle   = "## 历史摘要"
)

// historyItem 一条历史消息及其来源记录序号(用于裁剪后报告被裁区间)。
type historyItem struct {
	index    int // 来源记录序号;多条工具记录合成时为其中最大序号
	minIndex int // 来源记录的最小序号(工具折叠进同轮助手消息时可能小于 index)
	records  int // 来源记录条数(折叠的工具记录各算一条)
	msg      llmproto.ChatMessage
}

// AssembleContext 装配完整上下文。
//   - systemPrompt:   已构建好的系统提示(含 AGENTS/SOUL/... 各段)
//   - records:        历史文件读回的记录(按序号升序)
//   - currentUserMsg: 本轮用户消息(必须由调用方传入,避免与历史中的最后一轮重复)
//   - policy:         装配策略
//
// 返回的消息序列永远以 system 开头、以本轮用户消息结尾。
func AssembleContext(systemPrompt string, records []hCommon.MemoryRecord, currentUserMsg string, policy hCommon.ContextPolicy) hCommon.AssembleResult {
	policy = hCommon.NormalizeContextPolicy(policy)

	summaryText, active := splitSummaryAndActive(records)
	items := buildHistoryItems(active, policy.ToolReplayMaxRunes)

	system := harness.ComposeSystemMessage(systemPrompt, summaryText, historySummaryTitle) //composeSystemMessage(systemPrompt, summaryText)
	current := llmproto.ChatMessage{Role: hCommon.RoleUser, Content: currentUserMsg}

	result := hCommon.AssembleResult{Summary: summaryText}

	// 历史预算 = 窗口 - 输出预留 - 系统提示预留 - 工具 schema - 本轮用户消息,
	// 再取 min(…, MaxHistoryTokens)(历史回放上限)。
	// 窗口未知(ContextWindow<=0)时不裁剪,保持阶段1行为(不做任何预算推断)。
	var kept, dropped []historyItem
	if policy.ContextWindow <= 0 {
		kept = items
	} else {
		result.SystemReserveTokens = security.CalcSystemReservedTokens(policy, system)
		result.ToolsReserveTokens = policy.ToolsReserveTokens
		result.HistoryCapTokens = policy.MaxHistoryTokens

		budget := security.Budget(policy, system) - security.EstimateInputTokens([]llmproto.ChatMessage{current})
		if budget < 0 {
			budget = 0 // 窗口不够:本轮不带历史(而不是强行塞入历史把请求顶出窗口)
		}
		// 历史回放上限在"预算为正"的基础上再收一道:它只决定历史回放多少,
		// 不影响工具循环(FitMessages 用 Budget,不取这个 min)。
		if result.HistoryCapTokens > 0 && budget > result.HistoryCapTokens {
			budget = result.HistoryCapTokens
		}
		result.HistoryBudgetTokens = budget
		// 预算为 0 意味着"一条历史都放不下",此时压缩毫无意义:压缩不改变 system 预留,
		// 真正的问题是配置(SystemPromptReserveTokens / OutputReserve 相对窗口过大),要改配置而不是反复压缩。
		result.BudgetExhausted = budget <= 0
		kept, dropped, result.KeptHistoryTokens = trimHistoryItems(items, budget)
	}
	if len(dropped) > 0 {
		result.Trimmed = true
		result.DroppedCount = len(dropped)
		result.DroppedFrom, result.DroppedTo = dropped[0].minIndex, dropped[0].index
		for i := range dropped {
			result.DroppedRecordCount += dropped[i].records
			if dropped[i].minIndex < result.DroppedFrom {
				result.DroppedFrom = dropped[i].minIndex
			}
			if dropped[i].index > result.DroppedTo {
				result.DroppedTo = dropped[i].index
			}
		}
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
//   - 历史预算同样要扣除为系统提示预留的 SystemPromptReserveTokens(见 calcSystemReservedTokens)。
func FitMessages(llmProvider dataobject.TLlm, messages []llmproto.ChatMessage, policy hCommon.ContextPolicy) []llmproto.ChatMessage {
	if len(messages) <= 1 {
		return messages
	}

	policy = hCommon.NormalizeContextPolicy(policy)
	if policy.ContextWindow <= 0 { // 未配置窗口:不做裁剪,保持阶段1行为
		return messages
	}

	groups := groupMessages(messages[1:])
	if len(groups) <= 1 {
		return messages
	}

	//budget := policy.ContextWindow - policy.OutputReserve - CalcSystemReservedTokens(policy, messages[0])
	budget := security.Budget(policy, messages[0])
	if budget < 0 {
		budget = 0
	}

	// 从末组向前累加,直到超出预算(末组永远保留)
	total := 0
	keepFrom := len(groups) - 1
	for i := len(groups) - 1; i >= 0; i-- {
		groupTokens := security.EstimateInputTokens(groups[i])
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

// ============================== private functions & methods ==============================

// splitSummaryAndActive 拆出"摘要文本"与"仍需回放的记录"。
//
// 摘要记录(covers: from-to)取代了区间内的原始记录:被取代的记录不再回放,
// 否则同一段历史会既以摘要、又以原文出现,既浪费预算也干扰模型判断。
//
// "哪些摘要仍生效""哪些记录已被取代""注入文本长什么样"这三个判定**必须**用 hCommon 里的
// 共享实现(与写端 compactionRun / 合并判定 SummaryOverBudget 同一规则),
// 否则会出现"按 A 规则判断要不要压缩、按 B 规则决定注入什么"的漂移。
func splitSummaryAndActive(records []hCommon.MemoryRecord) (summaryText string, active []hCommon.MemoryRecord) {
	if len(hCommon.SummariesOf(records)) == 0 {
		return "", records
	}

	return hCommon.SummaryTextOf(records), hCommon.ActivePlainRecords(records)
}

// buildHistoryItems 把记录映射为历史消息(工具记录折叠进同轮助手消息)。
//
// 每条 item 同时记录其**来源记录**的最小序号与条数:压缩门槛要按"记录条数"判断
// (折叠后的消息条数会被工具记录放大),而 covers 区间要用**记录**序号表达。
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
			target.records += len(pending)
			if pending[0].Index < target.minIndex {
				target.minIndex = pending[0].Index
			}
		} else {
			// 该轮没有助手回复(如被中断):工具结果仍要保留,单独作为一条助手消息回放
			items = append(items, historyItem{
				index:    anchor,
				minIndex: pending[0].Index,
				records:  len(pending),
				msg:      llmproto.ChatMessage{Role: hCommon.RoleAssistant, Content: block},
			})
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
			items = append(items, historyItem{index: rec.Index, minIndex: rec.Index, records: 1,
				msg: llmproto.ChatMessage{Role: hCommon.RoleUser, Content: rec.Content}})
		case hCommon.HistoryRecordTypeTool: //mstorage.HistoryRecordTool:
			pending = append(pending, rec)
		case hCommon.HistoryRecordTypeAssistant: //mstorage.HistoryRecordAssistant:
			// 空助手消息(模型只输出思考内容、或内容被安全策略拦下)不回放:
			if strings.TrimSpace(rec.Content) == "" {
				continue
			}
			item := historyItem{index: rec.Index, minIndex: rec.Index, records: 1,
				msg: llmproto.ChatMessage{Role: hCommon.RoleAssistant, Content: rec.Content}}
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
//
// 每条都带**记录时间**前缀:历史工具结果原本与"刚查到的"长得一模一样,
// 模型会把旧数据当当下复述(实测:两天前的天气被当成今天)。时间由记录头解析而来
// (MemoryRecord.TimeText),缺失时降级为不带时间的旧格式。
func renderToolBlock(tools []hCommon.MemoryRecord, maxRunes int) string {
	var sb strings.Builder
	sb.WriteString(historyToolBlockOpen)
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Title)
		if name == "" {
			name = "tool"
		}
		body := pCommon.TruncateRunes(strings.TrimSpace(tool.Content), maxRunes, hCommon.TruncatedMarker)
		if timeText := strings.TrimSpace(tool.TimeText); timeText != "" {
			_, _ = fmt.Fprintf(&sb, "\n- [%s] %s: %s", timeText, name, body)
		} else {
			_, _ = fmt.Fprintf(&sb, "\n- %s: %s", name, body)
		}
	}
	sb.WriteString("\n")
	sb.WriteString(historyToolBlockClose)
	return sb.String()
}

// trimHistoryItems 在预算内保留尽可能新的历史(从尾部向前累加),返回保留与丢弃的两部分。
//
// 裁剪点必须落在"组边界"(user 消息)上:否则历史会以一条 orphan 的 assistant/tool 消息开头,
// 既容易让模型误判上下文,也可能与协议对消息顺序的要求冲突。
// budgetTokens <= 0 时返回"不带历史"(请求仍然合法:system + 本轮用户消息)。
func trimHistoryItems(items []historyItem, budgetTokens int) (kept, dropped []historyItem, keptTokens int) {
	if len(items) == 0 {
		return nil, nil, 0
	}
	if budgetTokens <= 0 {
		return nil, items, 0 // 窗口不够:不带历史(而不是强行塞入把请求顶出窗口)
	}

	cut := 0 // 保留 items[cut:]
	total := 0
	for i := len(items) - 1; i >= 0; i-- {
		itemTokens := security.EstimateInputTokens([]llmproto.ChatMessage{items[i].msg})
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
		return items, nil, total
	}
	return items[cut:], items[:cut], total
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
