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
	"path/filepath"
	"sort"
	"strings"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/security"
	"my.favorite/cclawservice/llmproto"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/model/repo"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
)

// 工具结果块标记:既是给模型的语义分隔,也是"这段不是助手原话"的明确提示。
const (
	historyToolBlockOpen  = "<工具结果>"
	historyToolBlockClose = "</工具结果>"
	historySummaryTitle   = "## 历史摘要"

	// 渲染分隔符:最终拼接与预算记账**共用同一组常量**,否则"记账"与"实际"会再次漂移。
	// (都是 ASCII,所以 len() 即 rune 数)
	sectionTitlePrefix = "## "
	sectionSeparator   = "\n\n"
)

// historyItem 一条历史消息及其来源记录序号(用于裁剪后报告被裁区间)。
type historyItem struct {
	index    int // 来源记录序号;多条工具记录合成时为其中最大序号
	minIndex int // 来源记录的最小序号(工具折叠进同轮助手消息时可能小于 index)
	records  int // 来源记录条数(折叠的工具记录各算一条)
	msg      llmproto.ChatMessage
}

// BuildSystemPrompt 以正常会话烦诶组装 system prompt，包含skills文件
func BuildSystemPrompt(agent dataobject.TAgent, skillFiles []string) (*dataobject.SystemPrompt, error) {
	return BuildSystemPromptEx(agent, skillFiles, dataobject.BootstrapScopeNormal)
}

// BuildSystemPromptEx 按指定会话范围组装system prompt
func BuildSystemPromptEx(agent dataobject.TAgent, skillFiles []string, scope dataobject.BootstrapScope) (*dataobject.SystemPrompt, error) {
	return BuildSystemPromptExWithActiveSkills(agent, skillFiles, scope, nil)
}

// BuildSystemPromptExWithActiveSkills 组装 system prompt,并把**命中技能的正文(SOP)**
// 一并注入。
//
// activeSkillFiles:本轮意图门判定为相关的 SKILL.md 绝对路径(来自 tool.Activation.ActiveFiles);
// 传空时行为与 BuildSystemPromptEx 完全一致(一个正文都不注入)。
//
// 为什么要注入正文:此前 system prompt 里只有 Available Skills 目录(名称+版本+描述),
// 而 SKILL.md 正文里的流程与契约(三步链路、禁止传目录、对账门禁、落盘位置…)从未进过
// prompt,模型只能靠 AGENTS.md 的几句概要根据猜 —— 历次“参数传错/流程跑飞”的主因。
// 只注入命中的技能:未命中技能的正文对本轮无益,只会稀释注意力。
func BuildSystemPromptExWithActiveSkills(agent dataobject.TAgent, skillFiles []string, scope dataobject.BootstrapScope, activeSkillFiles []string) (*dataobject.SystemPrompt, error) {
	// SKILL.md 的唯一读盘 + 唯一 YAML 解析入口在 prompt repo(按文件指纹缓存)。
	skills, report := repo.GetPromptRepository().LoadSkills(skillFiles)
	report.LogSummary(fmt.Sprintf("prompt service: agent=%s scope=%s", agent.AgentId, scope.String()))

	return BuildSystemPromptWithSkills(agent, skills, scope, activeSkillFiles)
}

// BuildSystemPromptWithSkills 用**已解析**的技能文档组装 system prompt(技能侧不再读盘)。
//
// skills:prompt repo 的 LoadSkills 产物(front matter + 正文)。让调用方复用同一份文档,既省掉
// 重复的路径解析/stat,也保证"工具侧看到的关键词/force-call"与"prompt 侧注入的目录/正文"
// 出自**同一份数据**(不会出现两份缓存各自失效的漂移)。
//
// 主链路(prepareChat)走本入口;BuildSystemPromptExWithActiveSkills 自行加载后委托到本入口,
// 因此两条路径最终都落在同一段组装逻辑上。
func BuildSystemPromptWithSkills(agent dataobject.TAgent, skills []hCommon.SkillDocument, scope dataobject.BootstrapScope, activeSkillFiles []string) (*dataobject.SystemPrompt, error) {
	// 加载并缓存 agent 启动文件(文件fingerprint未变化则直接命中缓存)
	if err := repo.GetPromptRepository().LoadAgentBootstrap(agent); err != nil {
		return nil, err
	}

	return assembleSystemPrompt(agent, scope, activeSkillFiles, skills)
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

	system := ComposeSystemMessage(systemPrompt, summaryText, historySummaryTitle) //composeSystemMessage(systemPrompt, summaryText)
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

// assembleSystemPrompt 组装 system prompt 的全部 section。
//
// skills:本轮技能文档(prompt repo 已解析的产物)。不再按 agentId 从仓库取 —— 技能文档与
// agent 无关,同一份 SKILL.md 对所有 agent 都是同一份数据,调用方传入即可避免"按 agent 存"
// 带来的每轮重读盘与跨 agent 残留问题。
func assembleSystemPrompt(agent dataobject.TAgent, scope dataobject.BootstrapScope, activeSkillFiles []string, skills []hCommon.SkillDocument) (*dataobject.SystemPrompt, error) {
	workspaceRoot, _ := hCommon.BuildAgentBootstrapRoot(agent)
	documents := repo.GetPromptRepository().GetAllDocuments(agent.AgentId) // 已按规范顺序加载

	var missing, truncated []string
	sections := make([]dataobject.SystemPromptSection, 0, len(documents)+1)

	// ---- 启动文档(顺序:AGENTS -> SOUL -> IDENTITY -> USER -> TOOLS -> BOOTSTRAP -> MEMORY) ----
	for i := range documents {
		doc := &documents[i]
		if !hCommon.IsKindAllowedInScope(doc.Kind, scope) {
			continue
		}

		// 必需文件缺失:注入 [MISSING] 标记
		if doc.Missing {
			sections = append(sections, dataobject.SystemPromptSection{
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

		// 单文件预算:USER.md 独立 4K,其余 20K
		budget := hCommon.MaxBootstrapFileChars
		if doc.Kind == hCommon.AgentDocUser {
			budget = hCommon.MaxUserDocChars
		}
		if runeLen := len([]rune(content)); runeLen > budget {
			content = pCommon.TruncateRunes(content, budget, pCommon.BlankString)
			truncated = append(truncated, doc.FileName)
		}

		sections = append(sections, dataobject.SystemPromptSection{
			Title:   doc.FileName,
			Content: content,
		})
	}

	// ---- skill 元数据目录(仅元数据,正文按需加载) ----
	skillCatalog, skillsTruncated := renderSkillsCatalog(skills)
	if len(skillCatalog) > 0 {
		sections = append(sections, dataobject.SystemPromptSection{
			Title:   "Available Skills",
			Content: skillCatalog,
		})
		if skillsTruncated {
			truncated = append(truncated, "Available Skills")
		}
	}

	// ---- 命中技能的正文(SOP):只注入意图门判定相关的技能 ----
	// 放在目录之后、预算裁剪之前:既让模型先知道“有哪些技能”,又让它拿到“怎么做”的规程。
	for _, sop := range renderActiveSkillDocs(skills, activeSkillFiles, &truncated) {
		sections = append(sections, sop)
	}

	// ---- 单 section 硬上限:任何 section 都不得超过 MaxPromptSectionLen ----
	sections, truncated = capSections(sections, hCommon.MaxPromptSectionLen, truncated)

	// ---- 总预算控制:优先保留高优先级文件,超限从尾部压缩 ----
	totalCap := security.CalcBoostrapBudget(agent.BootstrapTotalChars)
	sections, truncated = truncateTailBudget(sections, totalCap, truncated)

	// 上限以 rune 表达、下游预算以 token 表达:把两者换算着打出来,
	// 否则"为什么历史少了 41k"要靠人肉反推(61440 rune ≈ 41k token)。
	if totalCap > 0 {
		logging.LogDebugF("prompt service: agent=%s scope=%s bootstrap budget: section_cap=%d runes(≈%d tokens) total_cap=%d runes(≈%d tokens) skill_cap=%d runes(≈%d tokens) truncated=%v",
			agent.AgentId, scope.String(),
			hCommon.MaxPromptSectionLen, security.CharsToTokens(hCommon.MaxPromptSectionLen),
			totalCap, security.CharsToTokens(totalCap),
			hCommon.MaxSkillsPromptChars, security.CharsToTokens(hCommon.MaxSkillsPromptChars), truncated)
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
		sections = append(sections, dataobject.SystemPromptSection{
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

	// ---- 当前时间(固定尾巴:始终生效、不受预算截断) ----
	// 为什么必须有:此前 system prompt 里没有任何"今天几号"的锚点,而历史回放不带时间语义,
	// 模型无法判断上下文里的日期是否过期 —— 实测出现过把两天前的天气结果当"今天"复述的问题。
	// 放在最末(而非开头)是为保住前缀缓存:动态内容越靠后,被它失效化的前缀越短。
	sb.WriteString("\n\n## 当前时间\n\n")
	sb.WriteString(pCommon.FormatCurrentTime())

	// ---- 输出语言要求(面向中文用户的统一约定;追加在提示词最末,始终生效、不受预算截断) ----
	sb.WriteString("\n\n## 输出语言\n\n")
	sb.WriteString("你面向中文用户。所有呈现给用户的输出一律使用简体中文:包括正式回复、思考/推理过程、" +
		"进度说明与工具调用说明;除非用户明确要求使用其他语言。")

	return &dataobject.SystemPrompt{
		AgentId:   agent.AgentId,
		Workspace: workspaceRoot,
		Scope:     scope,
		Sections:  sections,
		Prompt:    sb.String(),
		Length:    sb.Len(),
		Missing:   missing,
		Truncated: truncated,
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
func capSections(sections []dataobject.SystemPromptSection, limit int, truncated []string) ([]dataobject.SystemPromptSection, []string) {
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
func sectionOverheadRunes(sec dataobject.SystemPromptSection, first bool) int {
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
// 仍在预算外(有意):总预算之后追加的 `Bootstrap Context Notice` 与末尾「输出语言」尾巴,
// 以及它们各自贡献的那一个分隔符 —— 它们是固定开销,由调用方按需预留。
func truncateTailBudget(sections []dataobject.SystemPromptSection, cap int, truncated []string) ([]dataobject.SystemPromptSection, []string) {
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
	kept := make([]dataobject.SystemPromptSection, 0, len(sections))
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
			kept = append(kept, dataobject.SystemPromptSection{
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

// renderActiveSkillDocs 把“命中技能的正文”渲染成 section(一技能一段)。
//   - skills:            prompt repo 缓存的技能文档(含 Content = 已剥离 front matter 的正文)
//   - activeSkillFiles:  意图门命中的 SKILL.md 绝对路径
//   - truncated:         超限时把技能名记进去(会落成 Bootstrap Context Notice)
//
// 匹配用 filepath.Clean 后的绝对路径:activeSkillFiles 来自意图门(tool 层只是转发 prompt repo
// 解析出的 doc.SkillFile),两侧同源,仍做一层归一以防调用方自行拼路径时的大小写/分隔符差异。
func renderActiveSkillDocs(skills []hCommon.SkillDocument, activeSkillFiles []string, truncated *[]string) []dataobject.SystemPromptSection {
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

	out := make([]dataobject.SystemPromptSection, 0, len(want))
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
		if runeLen := len([]rune(content)); runeLen > hCommon.MaxActiveSkillDocRunes {
			content = pCommon.TruncateRunes(content, hCommon.MaxActiveSkillDocRunes, pCommon.BlankString)
			*truncated = append(*truncated, "Skill: "+name)
		}

		out = append(out, dataobject.SystemPromptSection{
			Title:   "Skill: " + name,
			Content: content,
		})
		if len(out) >= hCommon.MaxActiveSkillsPerTurn {
			break
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
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
	if len(catalog) > hCommon.MaxSkillsInPrompt {
		catalog = catalog[:hCommon.MaxSkillsInPrompt]
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
	if full := render(catalog, 0); runes(full) <= hCommon.MaxSkillsPromptChars {
		return full, false
	}

	// 二分查找能容纳全部 skill 的最大描述长度
	fits := func(descMax int) bool {
		return runes(render(catalog, descMax)) <= hCommon.MaxSkillsPromptChars
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
		if runes(render(candidate, lo)) > hCommon.MaxSkillsPromptChars {
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
	for _, t := range tools {
		name := strings.TrimSpace(t.Title)
		if name == "" {
			name = "tool"
		}
		body := pCommon.TruncateRunes(strings.TrimSpace(t.Content), maxRunes, hCommon.TruncatedMarker)
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
