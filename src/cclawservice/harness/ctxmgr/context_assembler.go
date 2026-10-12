/**
* 上下文读回装配
*
* 职责:把"系统提示 + 历史记忆(文件记录) + 本轮用户消息"装配成一次 LLM 调用所需的完整消息序列,
* 并在模型上下文窗口内做预算裁剪。它是"读回"的唯一入口,与 knowledge 的写入端一一对应。
*
* 装配形态(与写入端记录类型的映射):
*
*   user 记录      → role=user
*   tool 记录      → 折叠进"同轮 assistant 消息"的工具结果块(见下),不单独成消息
*   assistant 记录 → role=assistant(被中断的记录以 hCommon.InterruptedReplyNotice 前缀回放:
*                    正文是残缺的,不标注会被模型当成"已经说完的完整发言")
*   summary 记录   → 折叠进 system 消息的"历史摘要"段,并使其 covers 区间内的旧记录不再回放
* prompt 装配时	：
*	Agent启动文件(MEMORY.md被分为静态区域 + 动态区域)作为system prompt的最前面部分,充分利用LLM的prompt cache机制
*	当前时间/记忆自动区/摘要/SOP都挪入动态区
*	--- 以上的这种顺序是一种契约,但是目前这种契约尽在注释中,现在加上运行时护栏,把契约约定,在context_guard.go文件中实现
**/

package ctxmgr

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"my.favorite/cclawservice/harness/api/llmproto"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/security"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/request"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/common/hash"
	"my.favorite/platformutility/logging"
)

// 工具结果块标记:既是给模型的语义分隔,也是"这段不是助手原话"的明确提示。
const (
	historyToolBlockOpen  = "<工具结果>"
	historyToolBlockClose = "</工具结果>"

	// 渲染分隔符:最终拼接与预算记账**共用同一组常量**,否则"记账"与"实际"会再次漂移。
	// (都是 ASCII,所以 len() 即 rune 数)
	sectionTitlePrefix = "## "
	sectionSeparator   = "\n\n"
)

// toolsCatalogFooter 工具清单末尾的"事实源"提醒(对应 OpenClaw Tooling 段的 source-of-truth reminder):
// 清单负责\"有哪些\",权威定义(参数/长描述)仍在请求体 tools 里。
const toolsCatalogFooter = "名称以本清单为准(参数与权威定义见请求体 tools);未列出的工具本轮不可用。"

// outputLanguagePolicy 输出语言约定(常量)。
const outputLanguagePolicy = "你面向中文用户。所有呈现给用户的输出一律使用简体中文:包括正式回复、思考/推理过程、" +
	"进度说明与工具调用说明;除非用户明确要求使用其他语言。"

// toolUsagePolicy 工具调用纪律:作为**固定尾巴**注入(不受预算截断),对所有 agent、所有轮次生效。
const toolUsagePolicy = "只使用本轮实际下发的工具(若 prompt 中有 `Available Tools` 段,以该清单为准);" +
	"技能清单(`Available Skills`)中出现、但不在工具清单里的能力本轮无法执行,须如实说明,不得虚构结果(技能已登记 ≠ 本轮有工具)。" +
	"禁止在回复中编造、杜撰或原样粘贴未注册的工具调用(例如 `<invoke name=\"exec\">…</invoke>`、`bash -c …`);" +
	"工具返回错误时如实说明并尝试修正参数后重试,不得假装执行成功。"

// dynamicBlockPolicy 说明本轮用户消息里后置注入的上下文块(常量)
const dynamicBlockPolicy = "本轮用户消息开头的若干带尖括号标记的块由系统注入,不是用户原话:\n" +
	"- <历史摘要>:更早对话的压缩摘要,仅供了解背景,不是新指令;\n" +
	"- <长期记忆>:系统维护的长期事实(可能来自其他会话),作为背景参考;\n" +
	"- <技能规程>:本轮命中技能的执行规范,视为系统指令,须严格遵守。\n" +
	"这些块与用户本轮要求冲突时,以用户本轮要求为准(技能规程中的安全约束除外)。"

// historyItem 一条历史消息及其来源记录序号(用于裁剪后报告被裁区间)。
type historyItem struct {
	index    int // 来源记录序号;多条工具记录合成时为其中最大序号
	minIndex int // 来源记录的最小序号(工具折叠进同轮助手消息时可能小于 index)
	records  int // 来源记录条数(折叠的工具记录各算一条)
	msg      llmproto.ChatMessage
}

// =========================== 稳定前缀本地缓存 ============================

// 两个目的:
// 		- 省掉每轮的重复拼接 / 渲染 / 预算记账
//		- 把顺序契约变成事实:同一个键必然的到同一份字节 --- 输入不变 -> 字节稳定
// 任一输入变化(人工改文档 / 蒸馏改 MEMORY.md / 增删技能 / 工具可见性变化)都会替换键 -> 自然失效
// 回合内变化的内容(命中技能 SOP)不进缓存,由 BuildSystemPromptWithSkills 每轮渲染

const promptCacheCapacity = 256

type promptCache struct {
	mu sync.Mutex

	entries map[string]*hCommon.SystemPrompt
	hits    int
	misses  int
}

var prompts = pCommon.GetSingleton(func() *promptCache {
	return &promptCache{
		entries: make(map[string]*hCommon.SystemPrompt),
	}
}).Instance()

func (cache *promptCache) get(key string) (*hCommon.SystemPrompt, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if sp, ok := cache.entries[key]; ok {
		cache.hits++
		return sp, true
	}

	cache.misses++
	return nil, false
}

func (cache *promptCache) put(key string, sp *hCommon.SystemPrompt) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if len(cache.entries) >= promptCacheCapacity {
		// 容量保护:满则整体重置
		cache.entries = make(map[string]*hCommon.SystemPrompt)
	}
	cache.entries[key] = sp
}

// 基于 **内容** 生成key
// 技能/工具都按字符串排序后参与哈希:入参顺序都懂不该导致缓存穿透
func promptKey(agent dataobject.TAgent, scope hCommon.BootstrapScope,
	skills []hCommon.SkillDocument, tools []hCommon.ToolPromptItem) string {
	var sb strings.Builder

	sb.WriteString(agent.AgentId)
	sb.WriteString("|")
	sb.WriteString(scope.String())
	sb.WriteString("|")
	fp, _ := GetAgentDocRepository().LoadAgentBootstrap(agent)
	sb.WriteString(fp)

	skillParts := make([]string, 0, len(skills))
	for i := range skills {
		skillParts = append(skillParts, skills[i].SkillFile+"#"+skills[i].Fingerprint)
	}
	sort.Strings(skillParts)
	sb.WriteString("|skills:")
	sb.WriteString(strings.Join(skillParts, ";"))

	toolParts := make([]string, 0, len(tools))
	for i := range tools {
		toolParts = append(toolParts, fmt.Sprintf("%s#%s#%d#%s",
			tools[i].Name, tools[i].Description, int(tools[i].Source), tools[i].SkillId))
	}
	sort.Strings(toolParts)
	sb.WriteString("|tools:")
	sb.WriteString(strings.Join(toolParts, ";"))

	sum, _ := hash.SHA256(sb.String())
	return sum[:16]
}

func getSystemPrompt(agent dataobject.TAgent, scope hCommon.BootstrapScope,
	skills []hCommon.SkillDocument, tools []hCommon.ToolPromptItem) (*hCommon.SystemPrompt, error) {
	key := promptKey(agent, scope, skills, tools)
	if sp, ok := prompts.get(key); ok {
		return sp, nil
	}

	// 未命中缓存
	sp, err := assembleSystemPrompt(agent, scope, nil, skills, tools)
	if err != nil {
		return nil, err
	}

	if v := ValidateStablePrompt(sp.Prompt); len(v) > 0 {
		logging.LogWarnF("prompt service: agent=%s stable system prompt contains volatile content: %v", agent.AgentId, v)
	}

	prompts.put(key, sp)
	return sp, nil
}

// =========================== 以下是构建prompt 和 LLM API Request请求部分===========================

// =========================== block for building prompt =================================

// BuildClassifySystemPrompt 构建用来通过用户问题的意图询问LLM返回合适Agent的 prompt
// 简介优先使用各 Agent 工作区 AGENTS.md 的摘要(已由调用方解析进 ClassifyAgent.Label)
func BuildClassifySystemPrompt(agents []ClassificationAgent) string {
	var sb strings.Builder
	sb.WriteString("你是一个智能体路由器。请根据用户的问题,从下面的智能体列表中选择最合适的一个。\n")
	sb.WriteString("智能体列表(格式: 编号|名称|简介):\n")
	for _, a := range agents {
		label := strings.TrimSpace(a.Label)
		if label == "" {
			label = strings.TrimSpace(a.AgentName)
		}
		_, _ = fmt.Fprintf(&sb, "- %s|%s|%s\n", a.AgentId, a.AgentName, label)
	}
	sb.WriteString("只回答最合适的智能体编号(如 agent_001),不要输出任何其他内容;如果不确定,回答 default。")
	return sb.String()
}

// BuildSystemPromptWithSkills 用**已解析**的技能文档组装 system prompt(技能侧不再读盘)
func BuildSystemPromptWithSkills(agent dataobject.TAgent, skills []hCommon.SkillDocument, scope hCommon.BootstrapScope,
	activeSkillFiles []string, tools []hCommon.ToolPromptItem) (*hCommon.SystemPrompt, error) {

	// 加载并缓存 agent 启动文件(文件fingerprint未变化则直接命中缓存)
	if _, err := GetAgentDocRepository().LoadAgentBootstrap(agent); err != nil {
		return nil, err
	}

	systemPrompt, err := getSystemPrompt(agent, scope, skills, tools)
	if err != nil {
		return nil, err
	}

	// 复制一份再挂SOP: 缓存对象是 **只读**,不能被本轮修改
	out := *systemPrompt
	out.Sections = append([]hCommon.SystemPromptSection(nil), systemPrompt.Sections...)

	var skillTruncated []string
	out.DynamicSkills = renderSectionsText(renderActiveSkillDocs(skills, activeSkillFiles, &skillTruncated), skillTruncated)

	return &out, nil
}

// ============================= block for building chat message & request ================================

// BuildSimpleChatMessage 以system/user 文本构建简单的prompt
func BuildSimpleChatMessage(sysMsg, userMsg string) []llmproto.ChatMessage {
	return []llmproto.ChatMessage{
		{Role: hCommon.RoleSystem, Content: sysMsg},
		{Role: hCommon.RoleUser, Content: userMsg},
	}
}

func BuildChatMessage(systemPrompt *hCommon.SystemPrompt, req *request.ChatRequest) []llmproto.ChatMessage {
	return []llmproto.ChatMessage{
		{Role: hCommon.RoleSystem, Content: systemPrompt.Prompt},
		{Role: hCommon.RoleUser, Content: req.ReqMsg},
	}
}

// BuildToolCallWithContentMessage 构造携带文本与工具调用的 assistant 消息(工具循环回填历史用)。
// 模型可能在一轮里既输出文本又请求工具(OpenAI 规范允许 content 与 tool_calls 并存),
// 文本须一并回填,否则下一轮 LLM 看不到自己刚说的话(上下文缺失)。
func BuildToolCallWithContentMessage(content string, calls []llmproto.ToolCall) llmproto.ChatMessage {
	return llmproto.ChatMessage{Role: hCommon.RoleAssistant, Content: content, ToolCalls: calls}
}

// BuildToolResultMessage 构造工具执行结果消息(role=tool,按 tool_call_id 关联)。
func BuildToolResultMessage(toolCallID, content string) llmproto.ChatMessage {
	return llmproto.ChatMessage{Role: hCommon.RoleTool, ToolCallID: toolCallID, Content: content}
}

func BuildChatCompletionRequestWithString(llmProvider dataobject.TLlm, sysMsg string, userMsg string, maxTokens int, stream bool) llmproto.ChatCompletionRequest {
	messages := BuildSimpleChatMessage(sysMsg, userMsg)
	req := llmproto.ChatCompletionRequest{
		Model:     llmProvider.GetModelName(),
		Messages:  messages,
		Stream:    stream,
		MaxTokens: nil,
	}
	if maxTokens > 0 {
		req.MaxTokens = &maxTokens
	}

	// 仅非流式请求按 provider 配置发送 enable_thinking(非流模式下是否思考);
	// 流式请求保持 nil:thinking 由流式默认开启并经 reasoning_content 返回,不在此关闭。
	if !stream {
		req.EnableThinking = new(llmProvider.ThkNonStreaming)
	}
	return req
}

// BuildChatCompletionRequest 构造 chat/completions 请求体。
// max_tokens 按输入 token 估算做动态计算,保证"估算输入 + max_tokens ≤ context_window",
// 避免长输入时因超出供应商上下文窗口而报错(同步/流式两条路径共用)。
// 未配置 ContextWindow(0)时不发送 max_tokens,由供应商使用其默认输出上限。
func BuildChatCompletionRequest(llmProvider dataobject.TLlm, systemPrompt *hCommon.SystemPrompt, chatReq *request.ChatRequest, stream bool) llmproto.ChatCompletionRequest {
	messages := BuildChatMessage(systemPrompt, chatReq)
	req := llmproto.ChatCompletionRequest{
		Model:    llmProvider.GetModelName(),
		Messages: messages,
		Stream:   stream,
	}
	// 仅非流式请求按 provider 配置发送 enable_thinking;流式保持 nil(不关闭 thinking)
	if !stream {
		req.EnableThinking = new(llmProvider.ThkNonStreaming)
	}

	// 动态裁剪 max_tokens:仅当 ContextWindow 已配置时才计算,否则返回 nil
	maxTokens := security.DynamicMaxTokens(llmProvider, messages)
	if maxTokens == 0 {
		req.MaxTokens = nil //应用供应商的默认配置
	} else {
		req.MaxTokens = new(maxTokens)
	}

	return req
}

// BuildChatCompletionRequestWithTool 构造带工具的 chat/completions 请求体(tool_choice 不发送)。
// max_tokens 沿用与普通请求一致的动态推导(dynamicMaxTokens)。
func BuildChatCompletionRequestWithTool(llmProvider dataobject.TLlm, messages []llmproto.ChatMessage, tools []llmproto.Tool, stream bool) llmproto.ChatCompletionRequest {
	return BuildChatCompletionRequestWithToolChoice(llmProvider, messages, tools, pCommon.BlankString, stream)
}

// BuildChatCompletionRequestWithToolChoice 在带工具请求的基础上附上工具选择策略。
//   - toolChoice: llmproto.ToolChoice* 常量;空串则不发送 tool_choice 字段(等价 auto)
//
// 为什么需要:技能正文不注入 system prompt,模型看不到"必须调用工具"的流程要求,
// 可能直接用上下文里的历史工具结果作答 —— 对实时类工具(force-call)首轮必须强制调用。
func BuildChatCompletionRequestWithToolChoice(llmProvider dataobject.TLlm, messages []llmproto.ChatMessage, tools []llmproto.Tool, toolChoice string, stream bool) llmproto.ChatCompletionRequest {
	req := llmproto.ChatCompletionRequest{
		Model:      llmProvider.GetModelName(),
		Messages:   messages,
		Stream:     stream,
		Tools:      tools,
		ToolChoice: strings.TrimSpace(toolChoice),
	}
	// 仅非流式请求按 provider 配置发送 enable_thinking;流式保持 nil(不关闭 thinking)
	if !stream {
		req.EnableThinking = new(llmProvider.ThkNonStreaming)
	}
	// 例外:强制调用(tool_choice=required)与 thinking 互斥,必须显式关闭思考(流式也要发)。
	if req.ToolChoice == llmproto.ToolChoiceRequired {
		req.EnableThinking = new(false)
	}

	mt := security.DynamicMaxTokens(llmProvider, messages)
	if mt == 0 {
		req.MaxTokens = nil // 应用供应商默认
	} else {
		req.MaxTokens = &mt
	}
	return req
}

// BuildDistillUserPrompt 拼装蒸馏输入:「已有长期记忆」(可空) + 本次新增对话。
func BuildDistillUserPrompt(existingMemory []string, batch []MemoryRecord, newLines []string) string {
	if len(batch) == 0 {
		return pCommon.BlankString
	}

	var sb strings.Builder
	if len(existingMemory) > 0 {
		sb.WriteString(DistillExistingHeader)
		for _, fact := range existingMemory {
			sb.WriteString(pCommon.ConcatStringWithLineSpace(fact)) //渲染一条事实行
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	_, _ = fmt.Fprintf(&sb, DistillUserHeader, batch[0].Index, batch[len(batch)-1].Index)
	for _, line := range newLines {
		sb.WriteString(line)
	}
	_, _ = fmt.Fprintf(&sb, "\n"+DistillRewriteNotice, hCommon.DistillMaxMemoryFacts, hCommon.DistillMaxFactRunes, hCommon.DistillNoneMark)
	return sb.String()
}

// BuildDistillAppendUserPrompt 追加模式的输入:给本次新增对话(不带记忆--本地去重兜底)
func BuildDistillAppendUserPrompt(batch []MemoryRecord, newLines []string) string {
	if len(batch) == 0 {
		return pCommon.BlankString
	}

	var sb strings.Builder
	batchLen := len(batch)
	_, _ = fmt.Fprintf(&sb, DistillUserHeader, batch[0].Index, batch[batchLen-1].Index)
	for _, line := range newLines {
		sb.WriteString(line)
	}

	_, _ = fmt.Fprintf(&sb, "\n"+DistillAppendNotice, hCommon.DistillNoneMark)
	return sb.String()
}

// DegradeChatRequest 错误信息中包含的信息对chat request降级
func DegradeChatRequest(req *llmproto.ChatCompletionRequest, err error) bool {
	if req == nil || err == nil {
		return false
	}

	if req.ToolChoice == pCommon.BlankString && req.EnableThinking == nil && req.StreamOptions == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "tool_choice") && !strings.Contains(msg, "enable_thinking") &&
		!strings.Contains(msg, "stream_options") && !strings.Contains(msg, "include_usage") {
		return false
	}

	logging.LogWarnF("llm client: provider rejected tool forced-call params (clear tool_choice=%s/enable_thinking), retry: %s",
		req.ToolChoice, err.Error())

	//对强制工具调用进行降级
	req.ToolChoice = pCommon.BlankString
	req.EnableThinking = nil
	req.StreamOptions = nil //代价:LLM供应商流式拿不到usage,将退化为"不可观测",而非报错

	return true
}

// AssembleContext 装配完整上下文。
//   - systemPrompt:   已构建好的系统提示(含 AGENTS/SOUL/... 各段)
//   - records:        历史文件读回的记录(按序号升序)
//   - currentUserMsg: 本轮用户消息(必须由调用方传入,避免与历史中的最后一轮重复)
//   - policy:         装配策略
//
// 返回的消息序列永远以 system 开头、以本轮用户消息结尾。
func AssembleContext(systemPrompt string, records []MemoryRecord, currentUserMsg string, turn TurnContext, policy hCommon.ContextPolicy) hCommon.AssembleResult {
	policy = NormalizeContextPolicy(policy)

	summaryText, active := splitSummaryAndActive(records)
	items := buildHistoryItems(active, policy.ToolReplayMaxRunes)

	turn.Summary = summaryText
	system := llmproto.ChatMessage{Role: hCommon.RoleSystem, Content: systemPrompt}
	current := llmproto.ChatMessage{Role: hCommon.RoleUser, Content: turn.Render(currentUserMsg)}

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
		// 不影响工具循环(FitMessages 用 Budget,不取这个 min)
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

	policy = NormalizeContextPolicy(policy)
	if policy.ContextWindow <= 0 { // 未配置窗口:不做裁剪,保持阶段1行为
		return messages
	}

	groups := groupMessages(messages[1:])
	if len(groups) <= 1 {
		return messages
	}

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

// ================================ private functions & methods for the module==============================

// section 顺序(前面稳定、后面易变,便于上游前缀缓存):
//
//	启动文档(AGENTS→…→MEMORY) → Available Skills → Available Tools → Skill: <name> → 截断提示
//
// 末尾追加**常量**固定尾巴(输出语言 / 工具调用纪律),不受预算截断。
//
// 【前缀缓存契约】system 必须字节稳定:上游按最长公共前缀命中,一个字节不同,
// 其后的整段历史与本轮消息都会失效。故"当前时间"不在 system 内(随本轮 user 注入),
// 常量段排在所有易变内容之后。
func assembleSystemPrompt(agent dataobject.TAgent, scope hCommon.BootstrapScope, activeSkillFiles []string,
	skills []hCommon.SkillDocument, tools []hCommon.ToolPromptItem) (*hCommon.SystemPrompt, error) {
	workspaceRoot, _ := hCommon.BuildAgentBootstrapRoot(agent)
	documents := GetAgentDocRepository().GetAgentDocuments(agent.AgentId) // 已按规范顺序加载

	var missing, truncated []string
	dynamicMemory := pCommon.BlankString
	dynamicSkills := pCommon.BlankString
	sections := make([]hCommon.SystemPromptSection, 0, len(documents)+1)

	// ---- 启动文档(顺序:AGENTS -> SOUL -> IDENTITY -> USER -> TOOLS -> BOOTSTRAP -> MEMORY) ----
	for i := range documents {
		doc := &documents[i]
		if !IsKindAllowedInScope(doc.Kind, scope) {
			continue
		}

		// 必需文件缺失:注入 [MISSING] 标记
		if doc.Missing {
			sections = append(sections, hCommon.SystemPromptSection{
				Title:   doc.FileName,
				Content: hCommon.MissingFileMarker + " Expected at: " + filepath.Join(workspaceRoot, doc.FileName),
			})
			missing = append(missing, doc.FileName)
			continue
		}

		content := strings.TrimSpace(doc.Content)
		if len(content) == 0 {
			continue // 空白文件跳过
		}

		//MEMORY.md 拆分:人工区进稳定前缀,自动区 **后置**(不在此注入)
		//蒸馏会改写自动区,留在 system 里 -> 每次蒸馏都让 "system 之后的整段历史"缓存失效
		if doc.Kind == hCommon.AgentDocMemory {
			human, auto := SplitMemoryDoc(content)
			dynamicMemory = auto
			content = strings.TrimSpace(human)
			if len(content) == 0 {
				continue //只剩自动区时,稳定前缀里不留空段
			}
		}

		// 单文件预算:USER.md 独立 4K,其余 20K
		budget := hCommon.MaxBootstrapFileChars
		if doc.Kind == hCommon.AgentDocUser {
			budget = hCommon.MaxUserDocChars
		}
		if runeLen := len([]rune(content)); runeLen > budget {
			content = pCommon.TruncateRunes(content, budget, pCommon.BlankString)
			truncated = append(truncated, doc.FileName)
		}

		sections = append(sections, hCommon.SystemPromptSection{
			Title:   doc.FileName,
			Content: content,
		})
	}

	// ---- skill 元数据目录(仅元数据,正文按需加载) ----
	skillCatalog, skillsTruncated := renderSkillsCatalog(skills)
	if len(skillCatalog) > 0 {
		sections = append(sections, hCommon.SystemPromptSection{
			Title:   "Available Skills",
			Content: skillCatalog,
		})
		if skillsTruncated {
			truncated = append(truncated, "Available Skills")
		}
	}

	// ---- 本轮实际下发的工具清单(名字 + 一句话用途 + 归属技能) ----
	//
	// 紧邻 Available Skills:两者都是"能力清单",且都只由**配置**(技能绑定 / t_tool.visible)
	// 决定、与本轮问题无关 ⇒ 属于稳定前缀。
	// 刻意排在 Skill 正文之前:尾部预算从后往前丢,而技能正文有单技能上限兜底;
	// 工具清单一旦被丢光,就会退化成"模型不知道本轮有哪些工具"的事故。
	toolCatalog, toolsTruncated := renderToolsCatalog(tools)
	if len(toolCatalog) > 0 {
		sections = append(sections, hCommon.SystemPromptSection{
			Title:   "Available Tools",
			Content: toolCatalog,
		})
		if toolsTruncated {
			truncated = append(truncated, "Available Tools")
		}
	}

	// ---- 命中技能的正文(SOP):只注入意图门判定相关的技能 ----
	var skillTruncated []string
	dynamicSkills = renderSectionsText(renderActiveSkillDocs(skills, activeSkillFiles, &skillTruncated), skillTruncated)

	// ---- 单 section 硬上限:任何 section 都不得超过 MaxPromptSectionLen ----
	sections, truncated = capSections(sections, MaxPromptSectionLen, truncated)

	// ---- 总预算控制:优先保留高优先级文件,超限从尾部压缩 ----
	totalCap := security.CalcBoostrapBudget(agent.BootstrapTotalChars)
	sections, truncated = truncateTailBudget(sections, totalCap, truncated)

	// 上限以 rune 表达、下游预算以 token 表达:把两者换算着打出来,
	// 否则"为什么历史少了 41k"要靠人肉反推(61440 rune ≈ 41k token)。
	if totalCap > 0 {
		logging.LogDebugF("prompt service: agent=%s scope=%s bootstrap budget: section_cap=%d runes(≈%d tokens) total_cap=%d runes(≈%d tokens) skill_cap=%d runes(≈%d tokens) truncated=%v",
			agent.AgentId, scope.String(),
			MaxPromptSectionLen, security.CharsToTokens(MaxPromptSectionLen),
			totalCap, security.CharsToTokens(totalCap),
			MaxSkillsPromptChars, security.CharsToTokens(MaxSkillsPromptChars), truncated)
	}

	// MEMORY.md 在注入顺序里是**最后**一段,总预算吃紧时它是第一个被截断/丢弃的。
	// 蒸馏按 DistillMemoryDocMaxChars 往这个文件里写长期记忆,一旦被挤掉,
	// 就会出现"事实写进了文件、却从没进过 prompt"——必须让它可见。
	for i := range truncated {
		if truncated[i] == hCommon.MemoryDocFileName {
			logging.LogWarnF("prompt service: agent=%s long-term memory (%s) was truncated by the prompt budget; distilled facts may never reach the prompt",
				agent.AgentId, hCommon.MemoryDocFileName)
			break
		}
	}

	// ---- 截断提示(OpenClaw: "## Bootstrap Context Notice") ----
	if len(truncated) > 0 {
		notice := "⚠️ Some bootstrap files were truncated to stay within the prompt budget: " +
			strings.Join(pCommon.UniqueStrings(truncated), ", ") + "."
		sections = append(sections, hCommon.SystemPromptSection{
			Title:   "Bootstrap Context Notice",
			Content: notice,
		})
	}

	// ---- 拼接最终 prompt(分隔符与预算记账共用 sectionTitlePrefix/sectionSeparator) ----
	var sb strings.Builder
	for i := range sections {
		if i > 0 {
			sb.WriteString(sectionSeparator)
		}
		if len(sections[i].Title) > 0 {
			sb.WriteString(sectionTitlePrefix)
			sb.WriteString(sections[i].Title)
			sb.WriteString(sectionSeparator)
		}
		sb.WriteString(sections[i].Content)
	}

	// ---- 固定输出语言 ----
	sb.WriteString("\n\n## 输出语言\n\n")
	sb.WriteString(outputLanguagePolicy)

	// ---- 固定工具调用 -----
	sb.WriteString("\n\n## 工具调用\n\n")
	sb.WriteString(toolUsagePolicy)

	// ---- 固定上下文块
	sb.WriteString("\n\n## 上下文块\n\n")
	sb.WriteString(dynamicBlockPolicy)

	return &hCommon.SystemPrompt{
		AgentId:       agent.AgentId,
		Workspace:     workspaceRoot,
		Scope:         scope,
		Sections:      sections,
		Prompt:        sb.String(),
		Length:        sb.Len(),
		DynamicMemory: dynamicMemory,
		DynamicSkills: dynamicSkills,
		Missing:       missing,
		Truncated:     truncated,
	}, nil
}

// capSections 对单个 section 施加硬上限(rune):任何 section 都不得超过 limit。
// limit <= 0 表示**该上限关闭**(不截断)。
//
// 【它是兜底层,不是第一道防线】当前每个 section 都有更紧的来源上限:
//   - 启动文档:MaxBootstrapFileChars(20480 rune),USER.md 用 MaxUserDocChars(4096 rune);
//   - Available Skills:MaxSkillsPromptChars(18000 rune);
//   - 通知 / 输出语言尾巴:极短。
//
// 三者都小于 MaxPromptSectionLen,因此本函数在当前常量下**不会截断任何东西** ——
// 它的价值是将来新增一种没有来源上限的 section 时兜底。改动上述任一常量前,
// 先看 TestSectionCapIsBackstopOnly(那条测试把这个前提显式化了)。
func capSections(sections []hCommon.SystemPromptSection, limit int, truncated []string) ([]hCommon.SystemPromptSection, []string) {
	if limit <= 0 {
		return sections, truncated
	}

	for i := range sections {
		if len([]rune(sections[i].Content)) <= limit {
			continue
		}
		sections[i].Content = pCommon.TruncateRunes(sections[i].Content, limit, pCommon.BlankString)
		truncated = append(truncated, sections[i].Title)
	}

	return sections, truncated
}

// sectionOverheadRunes section 除正文之外的开销(标题 + 段间分隔),与最终拼接严格一致。
// first=true 表示它是输出里的第一段(不加分隔符)。
func sectionOverheadRunes(sec hCommon.SystemPromptSection, first bool) int {
	overhead := 0
	if len(sec.Title) > 0 {
		overhead += len(sectionTitlePrefix) + len([]rune(sec.Title)) + len(sectionSeparator)
	}
	if !first {
		overhead += len(sectionSeparator)
	}

	return overhead
}

// truncateTailBudget 对全部注入内容施加总预算(rune):从前向后保留高优先级文件,
// 超限时从尾部截断/丢弃,并返回被截断的文件清单。
//
// cap <= 0 表示**该上限关闭**(不截断,直接返回)。该分支由 CalcBoostrapBudget(负数配置)进入 ——
// 三态约定与 max_history_tokens 一致:0=未配置→默认、>0=显式值、<0=不限制。
//
// 记账口径 = **最终 prompt 里实际占用的 rune**(正文 + 标题 `## <Title>` + 段间分隔),
// 以前只算正文,于是实际 prompt 会比 cap 多出「#section × (标题+分隔)」。
//
// 仍在预算外(有意):总预算之后追加的 `Bootstrap Context Notice` 与末尾固定尾巴
// (`当前时间`/`输出语言`/`工具调用`),以及它们各自贡献的那一个分隔符 ——
// 它们是固定开销,由调用方按需预留。
func truncateTailBudget(sections []hCommon.SystemPromptSection, cap int, truncated []string) ([]hCommon.SystemPromptSection, []string) {
	if cap <= 0 {
		return sections, truncated
	}

	total := 0
	for i := range sections {
		total += len([]rune(sections[i].Content)) + sectionOverheadRunes(sections[i], i == 0)
	}
	if total <= cap {
		return sections, truncated
	}

	budget := cap
	kept := make([]hCommon.SystemPromptSection, 0, len(sections))
	for i := range sections {
		// first 以"它在输出里的位置"为准(已保留数),而非输入下标:
		// 若前面有 section 连标题都放不下而被整体丢弃,它就不是第一段了。
		first := len(kept) == 0
		overhead := sectionOverheadRunes(sections[i], first)
		runeLen := len([]rune(sections[i].Content))
		if runeLen+overhead <= budget {
			budget -= runeLen + overhead
			kept = append(kept, sections[i])
			continue
		}

		// 该文件超出剩余预算:先扣掉标题/分隔开销,余下留给正文;连标题都放不下则整体丢弃。
		// 一旦截断,预算即耗尽(后续 section 只可能更放不下)。
		if available := budget - overhead; available > 0 {
			kept = append(kept, hCommon.SystemPromptSection{
				Title:   sections[i].Title,
				Content: pCommon.TruncateRunes(sections[i].Content, available, pCommon.BlankString),
			})
			truncated = append(truncated, sections[i].Title)
			budget = 0
		} else {
			truncated = append(truncated, sections[i].Title)
		}
	}

	return kept, truncated
}

// renderToolsCatalog 渲染"本轮可调用工具"清单:一行一个工具(Name + 归属 + 一句话用途)。
// 预算两层(与 renderSkillsCatalog 同口径,单位一律 rune):
//  1. 单条描述截断到 MaxToolDescPromptRunes —— 只影响这份\"清单\",完整描述仍在 schema 里;
//  2. 仍超 MaxToolsPromptChars 则逐条丢弃尾部工具。
//
// 返回 (清单文本, 是否被截断)。
func renderToolsCatalog(tools []hCommon.ToolPromptItem) (string, bool) {
	if len(tools) == 0 {
		return pCommon.BlankString, false
	}

	items := make([]hCommon.ToolPromptItem, 0, len(tools))
	for i := range tools {
		name := strings.TrimSpace(tools[i].Name)
		if len(name) == 0 {
			continue // 无名字无法定位工具:schema 侧本就不会下发,这里也跳过
		}
		item := tools[i]
		item.Name = name
		items = append(items, item)
	}
	if len(items) == 0 {
		return pCommon.BlankString, false
	}

	// 1) 单条描述上限:加省略号让"这里是节选"可见 —— 但不计入 truncated:
	// 完整描述仍在 schema 里,不该因此触发 Bootstrap Context Notice(那不是内容丢失)。
	for i := range items {
		if len([]rune(items[i].Description)) > MaxToolDescPromptRunes {
			items[i].Description = pCommon.TruncateRunes(items[i].Description, MaxToolDescPromptRunes, "…")
		}
	}

	runes := func(s string) int { return len([]rune(s)) }
	render := func(list []hCommon.ToolPromptItem) string {
		var sb strings.Builder
		for i := range list {
			sb.WriteString("- **")
			sb.WriteString(list[i].Name)
			sb.WriteString("**")
			if tag := list[i].OwnerTag(); len(tag) > 0 {
				sb.WriteString("(")
				sb.WriteString(tag)
				sb.WriteString(")")
			}
			if desc := strings.TrimSpace(list[i].Description); len(desc) > 0 {
				sb.WriteString(" — ")
				sb.WriteString(desc)
			}
			sb.WriteString("\n")
		}
		sb.WriteString(toolsCatalogFooter)
		return sb.String()
	}

	if full := render(items); runes(full) <= MaxToolsPromptChars {
		return full, false
	}

	// 2) 描述已压到上限仍超总预算:按下发顺序保留前面的工具,丢弃尾部。
	//(三索引切片:每轮候选都另起底层数组,避免 append 复用 keep 的底层数组造成误改)
	keep := make([]hCommon.ToolPromptItem, 0, len(items))
	for i := range items {
		candidate := append(keep[:len(keep):len(keep)], items[i])
		if runes(render(candidate)) > MaxToolsPromptChars {
			break
		}
		keep = candidate
	}
	if len(keep) == 0 {
		return pCommon.BlankString, true
	}

	return render(keep), true
}

// renderActiveSkillDocs 把“命中技能的正文”渲染成 section(一技能一段)。
//   - skills:            prompt repo 缓存的技能文档(含 Content = 已剥离 front matter 的正文)
//   - activeSkillFiles:  意图门命中的 SKILL.md 绝对路径
//   - truncated:         超限时把技能名记进去(会落成 Bootstrap Context Notice)
//
// 匹配用 filepath.Clean 后的绝对路径:activeSkillFiles 来自意图门(tool 层只是转发 prompt repo
// 解析出的 doc.SkillFile),两侧同源,仍做一层归一以防调用方自行拼路径时的大小写/分隔符差异。
func renderActiveSkillDocs(skills []hCommon.SkillDocument, activeSkillFiles []string, truncated *[]string) []hCommon.SystemPromptSection {
	if len(skills) == 0 || len(activeSkillFiles) == 0 {
		return nil
	}

	want := make(map[string]struct{}, len(activeSkillFiles))
	for _, f := range activeSkillFiles {
		if f = strings.TrimSpace(f); f != pCommon.BlankString {
			want[filepath.Clean(f)] = struct{}{}
		}
	}
	if len(want) == 0 {
		return nil
	}

	out := make([]hCommon.SystemPromptSection, 0, len(want))
	for i := range skills {
		doc := &skills[i]
		if _, ok := want[filepath.Clean(doc.SkillFile)]; !ok {
			continue
		}

		content := strings.TrimSpace(doc.Content)
		if len(content) == 0 {
			continue
		}

		name := strings.TrimSpace(doc.FrontMatter.Name)
		if name == pCommon.BlankString {
			name = filepath.Base(filepath.Dir(doc.SkillFile)) // 回退到技能目录名
		}

		// 单技能正文预算:超限截断并可见(而不是默默把一个技能的 SOP 截断一半还继续塞下一个)
		if runeLen := len([]rune(content)); runeLen > MaxActiveSkillDocRunes {
			content = pCommon.TruncateRunes(content, MaxActiveSkillDocRunes, pCommon.BlankString)
			*truncated = append(*truncated, "Skill: "+name)
		}

		out = append(out, hCommon.SystemPromptSection{
			Title:   "Skill: " + name,
			Content: content,
		})
		if len(out) >= MaxActiveSkillsPerTurn {
			break
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// 把section列表渲染成注入文本(与system的拼接格式一致)
// 并将"被截断"提示附在末尾 -- 该提示随技能集变化,必须留在后置块里,不能进system
func renderSectionsText(sections []hCommon.SystemPromptSection, truncated []string) string {
	var sb strings.Builder
	for i := range sections {
		if i > 0 {
			sb.WriteString(sectionSeparator)
		}

		if len(sections[i].Title) > 0 {
			sb.WriteString(sectionTitlePrefix)
			sb.WriteString(sections[i].Title)
			sb.WriteString(sectionSeparator)
		}

		sb.WriteString(sections[i].Content)
	}

	if note := strings.TrimSpace(strings.Join(pCommon.UniqueStrings(truncated), ", ")); note != pCommon.BlankString {
		if sb.Len() > 0 {
			sb.WriteString(sectionSeparator)
		}
		sb.WriteString("⚠️ 以下技能规程因超长被截断: " + note + "。")
	}

	return sb.String()
}

// renderSkillsCatalog 渲染 skill 元数据目录(参考 OpenClaw formatSkillsForPromptBounded):
//   - 仅输出元数据(名称 + 版本 + 描述),按名称排序,受数量与字符预算约束
//   - 优先完整描述;超出预算则先缩短描述,再逐条丢弃尾部 skill
//   - 返回 (目录文本, 是否被截断)
func renderSkillsCatalog(skills []hCommon.SkillDocument) (string, bool) {
	// 过滤空元数据
	catalog := make([]hCommon.SkillDocument, 0, len(skills))
	for i := range skills {
		meta := &skills[i].FrontMatter
		if len(meta.Name) == 0 && len(meta.Description) == 0 {
			continue
		}
		catalog = append(catalog, skills[i])
	}
	if len(catalog) == 0 {
		return "", false
	}

	// 按名称排序(OpenClaw localeCompare "en")
	sort.Slice(catalog, func(i, j int) bool {
		return strings.ToLower(catalog[i].FrontMatter.Name) < strings.ToLower(catalog[j].FrontMatter.Name)
	})

	// 数量上限
	if len(catalog) > MaxSkillsInPrompt {
		catalog = catalog[:MaxSkillsInPrompt]
	}

	render := func(list []hCommon.SkillDocument, descMax int) string {
		var sb strings.Builder
		for i := range list {
			meta := &list[i].FrontMatter
			sb.WriteString("- **")
			sb.WriteString(meta.Name)
			sb.WriteString("**")
			if len(meta.Version) > 0 {
				sb.WriteString(" (")
				sb.WriteString(meta.Version)
				sb.WriteString(")")
			}
			desc := meta.Description
			if descMax > 0 {
				desc = pCommon.TruncateRunes(desc, descMax, pCommon.BlankString)
			}
			if len(desc) > 0 {
				sb.WriteString(": ")
				sb.WriteString(desc)
			}
			sb.WriteString("\n")
		}
		return sb.String()
	}

	const minDesc = 4
	const maxDesc = 120

	// 预算一律按 **rune** 计(与 MaxBootstrapFileChars / MaxPromptSectionLen / 总预算同单位)。
	// 以前这里用的是 len(...)(字节):常量名叫 Chars 却按字节卡,中文描述实际只有约 1/3 的额度。
	runes := func(s string) int { return len([]rune(s)) }

	// 完整描述若可容纳,直接使用
	if full := render(catalog, 0); runes(full) <= MaxSkillsPromptChars {
		return full, false
	}

	// 二分查找能容纳全部 skill 的最大描述长度
	fits := func(descMax int) bool {
		return runes(render(catalog, descMax)) <= MaxSkillsPromptChars
	}

	lo, hi := minDesc, maxDesc
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if fits(mid) {
			lo = mid
		} else {
			hi = mid - 1
		}
	}

	if fits(lo) {
		return render(catalog, lo), true
	}

	// 最短描述也放不下:逐条丢弃尾部 skill 直到满足预算
	keep := make([]hCommon.SkillDocument, 0, len(catalog))
	for i := range catalog {
		candidate := append(keep, catalog[i])
		if runes(render(candidate, lo)) > MaxSkillsPromptChars {
			break
		}
		keep = candidate
	}
	if len(keep) == 0 {
		return "", true
	}
	return render(keep, lo), true
}

// splitSummaryAndActive 拆出"摘要文本"与"仍需回放的记录"。
//
// 摘要记录(covers: from-to)取代了区间内的原始记录:被取代的记录不再回放,
// 否则同一段历史会既以摘要、又以原文出现,既浪费预算也干扰模型判断。
//
// "哪些摘要仍生效""哪些记录已被取代""注入文本长什么样"这三个判定**必须**用 hCommon 里的
// 共享实现(与写端 compactionRun / 合并判定 SummaryOverBudget 同一规则),
// 否则会出现"按 A 规则判断要不要压缩、按 B 规则决定注入什么"的漂移。
func splitSummaryAndActive(records []MemoryRecord) (summaryText string, active []MemoryRecord) {
	if len(SummariesOf(records)) == 0 {
		return "", records
	}

	return SummaryTextOf(records), ActivePlainRecords(records)
}

// buildHistoryItems 把记录映射为历史消息(工具记录折叠进同轮助手消息)。
//
// 每条 item 同时记录其**来源记录**的最小序号与条数:压缩门槛要按"记录条数"判断
// (折叠后的消息条数会被工具记录放大),而 covers 区间要用**记录**序号表达。
func buildHistoryItems(records []MemoryRecord, toolMaxRunes int) []historyItem {
	items := make([]historyItem, 0, len(records))
	var pending []MemoryRecord

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
		case HistoryRecordTypeUser: //knowledge.HistoryRecordUser:
			// 上一条用户消息之前残留的工具记录(异常结构)不再挂到新问题上
			flush(nil, rec.Index)
			if strings.TrimSpace(rec.Content) == "" {
				continue // 空提问无信息量(异常数据),不回放
			}
			items = append(items, historyItem{index: rec.Index, minIndex: rec.Index, records: 1,
				msg: llmproto.ChatMessage{Role: hCommon.RoleUser, Content: rec.Content}})
		case HistoryRecordTypeTool: //knowledge.HistoryRecordTool:
			pending = append(pending, rec)
		case HistoryRecordTypeAssistant: //knowledge.HistoryRecordAssistant:
			// 空助手消息(模型只输出思考内容、或内容被安全策略拦下)不回放:
			content := strings.TrimSpace(rec.Content)
			if content == "" {
				continue
			}
			// 被中断的回复是**残缺**的:不标注,模型会把它当成完整发言
			// (接着半句话继续说、或以为已回答过而不再补充)。
			// 压缩/蒸馏侧已有 "assistant(被中断)" 标签(compaction.recordLabel),此处补齐同一语义。
			if rec.IsInterrupted() {
				content = InterruptedReplyNotice + "\n" + content
			}
			item := historyItem{index: rec.Index, minIndex: rec.Index, records: 1,
				msg: llmproto.ChatMessage{Role: hCommon.RoleAssistant, Content: content}}
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
func renderToolBlock(tools []MemoryRecord, maxRunes int) string {
	var sb strings.Builder
	sb.WriteString(historyToolBlockOpen)
	for _, t := range tools {
		name := strings.TrimSpace(t.Title)
		if name == "" {
			name = "tool"
		}
		body := pCommon.TruncateRunes(strings.TrimSpace(t.Content), maxRunes, TruncatedMarker)
		if timeText := strings.TrimSpace(t.TimeText); timeText != "" {
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
