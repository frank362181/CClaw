/**
* 记忆蒸馏(把会话里的"长期有效事实"写入 Agent 工作区的 MEMORY.md)
*
* 与压缩的区别:
*   - 压缩(compaction.go)面向**单个会话**:让本会话历史体积有界,摘要留在会话历史文件内;
*   - 蒸馏(distillation.go)面向**Agent 的长期记忆**:把跨会话仍然有效的事实沉淀到工作区文件,
*     由 prompt 组装层按固定顺序注入 system prompt(AGENTS → SOUL → ... → MEMORY)。
*
* **作用域(重要)**:MEMORY.md 位于 `<exeDir>/cclaw/<ownerId>/<agentDir>/`,按
* (租户, agent)共享 —— 同一单位下使用该 agent 的**所有用户**都会看到它的内容,并且它会被
* 注入每一次请求。因此蒸馏只允许写入**单位/agent 级**的长期事实(项目结构、口径、约定、决策),
* **不得写入个人隐私与用户个人信息**(提示词里有硬约束)。个人画像属于 USER.md。
**/

package mstorage

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	cCommon "my.favorite/cclawservice/common"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
)

// 蒸馏提示词
//
// 作用域约束是本提示词最重要的部分:MEMORY.md 是 (租户, agent) 级共享文件,
// 会被注入该 agent 的每一次请求。写错范围等于把用户个人信息变成所有人的 system prompt。
const (
	distillSystemPrompt = "你负责从对话中提炼**长期有效**的记忆,写入智能体的长期记忆文件。\n" +
		"这个文件由同一单位下的**所有人共用**,并会出现在后续每一次对话的开头,所以:\n" +
		"\n" +
		"【只提炼单位/智能体级、跨会话仍然成立的信息】例如:\n" +
		"- 项目与系统的稳定事实(目录结构、服务地址、命名与编码规范、字段口径);\n" +
		"- 业务规则与已确认的结论(为什么这么做、被否决的方案及原因);\n" +
		"- 本单位的固定约定(交付物格式、存放位置、术语用法);\n" +
		"- 尚未完成的长期待办。\n" +
		"\n" +
		"【绝对不要提炼】\n" +
		"- 个人隐私与用户个人信息:姓名、称呼、联系方式、账号、个人喜好(喜欢的花/城市/数字)、" +
		"个人行程与历史查询记录等 —— 这些不属于共享记忆,即使对话里反复出现也不要写;\n" +
		"- 一次性的内容:本次问答过程、临时计算结果、即时查询结果(天气/汇率/股价等)、寒暄。\n" +
		"\n" +
		"输出要求:\n" +
		"1. 每条一行,以 \"- \" 开头,一句话说清,不写理由过程;\n" +
		"2. 不得推测或补造,只依据给定对话;\n" +
		"3. 没有任何值得长期记住的内容时,只输出 " + hCommon.DistillNoneMark + "。"

	distillUserHeader = "以下是一段对话记录(记录序号 %d-%d),请提炼长期记忆:\n\n"

	// distillExistingHeader 已有长期记忆的表头。
	distillExistingHeader = "【已有长期记忆】(上次蒸馏得到的当前状态)\n"

	// distillRewriteNotice 收尾指令:要求输出"更新后的完整列表",并**显式给出长度上限**
	// (模型自己猜"简明"时容易写到上限之外,而超长输出会被 max_tokens 截断 → 整轮作废)。
	distillRewriteNotice = "请输出**更新后的完整长期记忆列表**(每行一条,以 \"- \" 开头):" +
		"保留仍然有效的条目;把被取代的改写为新说法;删掉已失效、已完成或一次性的条目;" +
		"合并重复表述;总计不超过 %d 条,每条不超过 %d 个字符;" +
		"没有任何需要记住的内容时只输出 %s。\n"
)

// distillSectionHeaderPattern 匹配小节标题 "## 2026-09-13 01:02:03 | session xxx"(可带 "| records a-b")
var distillSectionHeaderPattern = regexp.MustCompile(`(?m)^## \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}.*$`)

// distillRecordsRangePattern 匹配小节标题里的记录区间(旧小节没有这一段)。
var distillRecordsRangePattern = regexp.MustCompile(`\| records (\d+)-(\d+)\s*$`)

// DistillResult 蒸馏结果。
type DistillResult struct {
	Path       string   // MEMORY.md 路径(未写入时为空)
	Facts      []string // 本次写入后的自动区条目(即当前全部长期记忆)
	Skipped    int      // 本地判定为重复而丢弃的条数
	Updated    int      // 被改写为新说法的条数(同一槽位更新)
	Removed    int      // 模型未再输出、因而被移出长期记忆的旧条目数
	Migrated   int      // 迁移掉的旧格式小节数
	CoversFrom int      // 本次**判定过**的记录区间起点(0 表示未判定到任何区间)
	CoversTo   int      // 本次判定过的区间终点 —— 调用方据此判断"哪些原文可以被压缩吞掉"
	Backlog    int      // 因超出输入预算而留给下一轮的记录条数
}

// memoryDocPath 返回某 Agent 的长期记忆文件路径
func memoryDocPath(agent dataobject.TAgent) (string, bool) {
	root, ok := cCommon.BuildAgentBootstrapRoot(agent)
	if !ok {
		return pCommon.BlankString, false
	}
	return filepath.Join(root, cCommon.MemoryDocFileName), true
}

// ============================== watermark(增量蒸馏) ==============================

// parseDistillHeader 解析小节标题,兼容两种形式:
//
//	## 2026-09-14 10:00:00 | session sess-1
//	## 2026-09-14 10:00:00 | session sess-1 | records 41-60
//
// 第二种(带区间)才能推进 watermark;旧小节返回 from=to=0。
func parseDistillHeader(line string) (sessionId string, from, to int, ok bool) {
	line = strings.TrimSpace(line)
	if !distillSectionHeaderPattern.MatchString(line) {
		return pCommon.BlankString, 0, 0, false
	}

	const marker = "| session "
	start := strings.Index(line, marker)
	if start < 0 {
		return pCommon.BlankString, 0, 0, false
	}
	rest := line[start+len(marker):]

	// 会话号到下一个空格/竖线为止(避免 "sess-1" 命中 "sess-11")
	end := strings.IndexAny(rest, " |")
	if end < 0 {
		sessionId = strings.TrimSpace(rest)
	} else {
		sessionId = rest[:end]
	}
	if sessionId == pCommon.BlankString {
		return pCommon.BlankString, 0, 0, false
	}

	if m := distillRecordsRangePattern.FindStringSubmatch(line); m != nil {
		_, _ = fmt.Sscanf(m[1], "%d", &from)
		_, _ = fmt.Sscanf(m[2], "%d", &to)
	}
	return sessionId, from, to, true
}

// lastDistilledIndex 从内容(内容来自于MEMORY.md )中返回该会话里已蒸馏到的最大记录序号
// (0 = 还没蒸馏过)
// 两个来源取**较大值**:
//   - 格式:自动记忆区定界注释里的 `watermarks=sess:120,…`;
func lastDistilledIndex(content, sessionId string) int {
	if content == pCommon.BlankString || sessionId == pCommon.BlankString {
		return 0
	}

	lines := strings.Split(content, "\n")
	last := 0
	if begin, _, ok := autoBlockBounds(lines); ok {
		if m := distillAutoBeginPattern.FindStringSubmatch(strings.TrimSpace(lines[begin])); m != nil {
			last = parseAutoWatermarks(m[2])[sessionId]
		}
	}
	for _, line := range lines {
		id, _, to, ok := parseDistillHeader(line)
		if ok && id == sessionId && to > last {
			last = to
		}
	}
	return last
}

// pendingRecords 取出 watermark 之后、仍需蒸馏的记录(保持升序)。
func pendingRecords(records []hCommon.MemoryRecord, watermark int) []hCommon.MemoryRecord {
	pending := make([]hCommon.MemoryRecord, 0, len(records))
	for i := range records {
		if records[i].Index > watermark {
			pending = append(pending, records[i])
		}
	}
	return pending
}

// windowPrefix 从待蒸馏记录里取**连续的一段前缀**(从最旧开始),使其送入模型后不超过预算。
//
// 为什么取前缀而不是"最新的那一段":watermark 让蒸馏变成"队列消费" ——
// 取前缀保证已蒸馏区间始终**连续**、不留下永久空洞;取最新一段会把中间那段永久跳过。
// 溢出的部分留在队列里(下一轮继续),条数通过返回值交给调用方告警。
// 至少返回 1 条(哪怕它自己就超预算),否则队列永远推不动。
func windowPrefix(records []hCommon.MemoryRecord, budgetChars int) ([]hCommon.MemoryRecord, []string, int) {
	if len(records) == 0 {
		return nil, nil, 0
	}

	total, fit := 0, 0
	for i := range records {
		lineRunes := utf8.RuneCountInString(compactionPromptLine(records[i]))
		if i > 0 && total+lineRunes > budgetChars {
			break
		}
		total += lineRunes
		fit = i + 1
	}

	batch := records[:fit]
	lines := make([]string, 0, fit)
	for i := range batch {
		lines = append(lines, compactionPromptLine(batch[i]))
	}
	return batch, lines, len(records) - fit
}

// buildDistillUserPrompt 拼装蒸馏输入:「已有长期记忆」(可空) + 本次新增对话。
//
// batch 必须是 windowPrefix 给出的**连续一段**,newLines 是它渲染好的行:
// 表头区间、列出的内容、以及模型据此判断的"新增范围"三者必须完全一致。
func buildDistillUserPrompt(existingMemory []string, batch []hCommon.MemoryRecord, newLines []string) string {
	if len(batch) == 0 {
		return pCommon.BlankString
	}

	var sb strings.Builder
	if len(existingMemory) > 0 {
		sb.WriteString(distillExistingHeader)
		for _, fact := range existingMemory {
			sb.WriteString(pCommon.ConcatStringWithLineSpace(fact)) //渲染一条事实行
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	_, _ = fmt.Fprintf(&sb, distillUserHeader, batch[0].Index, batch[len(batch)-1].Index)
	for _, line := range newLines {
		sb.WriteString(line)
	}
	_, _ = fmt.Fprintf(&sb, "\n"+distillRewriteNotice, hCommon.DistillMaxMemoryFacts, hCommon.DistillMaxFactRunes, hCommon.DistillNoneMark)
	return sb.String()
}

// isExplicitNone 模型是否**明确**判定"没有值得长期记住的内容"。
//
// 与"输出不可解析"区分开:parseDistilledFacts 对两者都返回空,但只有明确 NONE 才允许清空
// 迁移期的旧条目;乱码/空响应必须走保守路径(保留旧记忆)。
func isExplicitNone(text string) bool {
	return strings.EqualFold(strings.TrimSpace(text), hCommon.DistillNoneMark)
}

// hasDurableSignal 本地预筛:这批记录里是否出现"可能值得长期记住"的信号。
//
// 动机:按轮次节拍无条件调用 LLM,会在"这 10 轮只是闲聊/查天气"时白花一次调用。
// 只看**用户发言**:长期有效的约定/口径/偏好都是用户提出的;助手与工具输出里
// 路径、编号遍地都是,拿它们当信号等于不过滤。
// 这是**粗筛**:宁可多调用一次,也不要漏掉真正该记住的内容。
func hasDurableSignal(records []hCommon.MemoryRecord) bool {
	for i := range records {
		if records[i].RecordType != hCommon.HistoryRecordTypeUser {
			continue
		}
		content := records[i].Content
		// 结构性信号:路径 / 文件名 / 长数字(与压缩的关键信息采样共用同一套模式)
		if keyTokenPattern.MatchString(content) {
			return true
		}
		for _, token := range distillSignalTokens {
			if strings.Contains(content, token) {
				return true
			}
		}
	}
	return false
}

// distillSignalTokens 本地预筛用的信号词(约定/规范/偏好类的表述)。
var distillSignalTokens = []string{
	"以后", "今后", "始终", "总是", "统一", "规范", "约定", "口径", "标准", "保持一致",
	"记住", "记为", "注意", "偏好", "习惯", "禁止", "不要", "必须", "对外",
	"项目", "目录", "路径", "命名", "编码", "账号", "单位", "术语", "流程", "模板", "交付",
}

// parseDistilledFacts 解析模型输出。
//
// 约定格式是每行一条 "- 事实"。**完全没有列表**时做一次谨慎回退(兼容不听话的模型),
// 但只接受"看起来是独立断言"的行,并打 WARN —— 回退本身就说明模型没遵守格式,
// 此时最容易把"以下是提炼出的长期记忆:"这类前言当成事实永久写进长期记忆。
func parseDistilledFacts(text string) []string {
	text = strings.TrimSpace(text)
	if text == pCommon.BlankString || strings.EqualFold(text, hCommon.DistillNoneMark) {
		return nil
	}

	var bulleted, plain []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == pCommon.BlankString || strings.EqualFold(line, hCommon.DistillNoneMark) ||
			strings.HasPrefix(line, "#") || strings.HasPrefix(line, "<!--") {
			continue
		}

		isBullet := strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "• ")
		// 注意:非列表行必须**原样**判定 —— 之前无条件 TrimLeft(line, "-*•") 会把
		// "**标题**" 的星号削掉,导致后面的 "**" 守卫形同虚设。
		item := line
		if isBullet {
			item = strings.TrimSpace(strings.TrimLeft(line, "-*•"))
		}
		if item == pCommon.BlankString {
			continue
		}
		if isBullet {
			bulleted = append(bulleted, item)
			continue
		}
		if pCommon.LooksLikeFact(item, hCommon.DistillMinFactRunes) {
			plain = append(plain, item)
		}
	}

	items := bulleted
	if len(items) == 0 && len(plain) > 0 {
		items = plain
		logging.LogWarnF("chat memory distill: model output has no bullet list, %d plain lines accepted as facts", len(plain))
	}

	items = pCommon.DedupeFacts(items, hCommon.DistillSimilarFactRatio, hCommon.DistillSimilarFactMinRunes)

	facts := make([]string, 0, len(items))
	for _, item := range items {
		facts = append(facts, pCommon.TruncateRunes(item, hCommon.DistillMaxFactRunes, hCommon.TruncatedMarker))
		if len(facts) >= hCommon.DistillMaxMemoryFacts {
			break
		}
	}
	return facts
}

// memoryDocWrite 一次"整体重写自动记忆区"的请求。
//
// 参数多且语义相近(两个区间端点、两个条目列表),用结构体让调用点自解释 ——
// 尤其是 snapshot 与 explicitNone 这两个带并发/迁移语义的标志位。
type memoryDocWrite struct {
	path         string
	facts        []string // 模型给出的"更新后的完整列表"
	sessionId    string
	fromIndex    int
	toIndex      int
	maxChars     int
	snapshot     []string // 构建提示词时给模型看过的旧条目(用于区分"模型丢弃"与"并发新增")
	explicitNone bool     // 模型明确输出 NONE(仅在"旧格式遗留"文件上允许清空条目)
}

// appendFactsToMemoryDoc 用一个**整体重写**的自动记忆区替换原有的自动内容。
//
// 为什么是"重写"而不是"追加 + 满了删最旧":
// MEMORY.md 是**长期记忆**,不是日志。追加 + 最旧优先删除会让它退化成"最近 N 次蒸馏的滑动窗口",
// 而且删的是**最早写入、往往最长期有效**的事实(语言偏好、单位口径),留下的却是最新的琐事。
// 现在的语义是:自动区 = 当前有效的长期记忆列表(由模型在上次列表基础上增量更新),
// 因此体积天然有界、不再需要按最旧裁剪,人工维护区也永远不会被碰到。
//
// 两处与"并发/迁移"相关的判定在持锁后重算(不能沿用提示词构建时的快照):
//   - snapshot 是提示词里给模型看过的旧条目。"被模型丢弃"只能对快照里的条目计数 ——
//     本轮开始之后另一个会话写入的条目模型根本没看过,按文件当前内容计数会把它们误删;
//   - 迁移期(区域里只有旧格式小节、还没有自动区定界注释)信任模型的范围判断,
//     不套用安全阀:范围从"用户偏好"改成"单位/agent 级事实"后,旧条目的**正确结局就是被大面积删除**,
//     安全阀会让这批越界内容永远钉在共享文件里。
func appendFactsToMemoryDoc(req memoryDocWrite) (written []string, skipped, updated, removed, migrated int, err error) {
	unlock := GetStorageLocks().Lock(hCommon.BuildMemoryDocLockKey(req.path))
	defer unlock()

	existing, readErr := pCommon.ReadTextIfExists(req.path)
	if readErr != nil {
		return nil, 0, 0, 0, 0, readErr
	}

	lines := strings.Split(existing, "\n")
	oldFacts, migrated := existingMemoryFacts(lines)

	// 迁移期两条特殊规则(只在还没有自动区定界注释的文件上生效)
	legacyOnly := !hasAutoBlock(lines)
	facts := req.facts
	if legacyOnly && len(oldFacts) > 0 {
		logging.LogWarnF("chat memory distill: migrating legacy sections of %s (%d facts), accept the model's list as-is",
			req.path, len(oldFacts))
		if req.explicitNone {
			// 模型明确判定"没有值得长期记住的内容":旧格式的条目本就不属于共享长期记忆
			// (作用域已收紧),允许清空内容 —— 但**保留定界注释与水位**,否则下一轮会重复蒸馏同一段对话。
			facts = nil
		}
	}

	// 模型输出的本地兜底去重(模型偶尔会把同一件事写两遍或换个说法写一遍)
	next := make([]string, 0, len(facts))
	known := make(map[string]bool, len(facts))
	for _, fact := range facts {
		key := pCommon.NormalizeFact(fact)
		if key == pCommon.BlankString {
			skipped++
			continue
		}
		if known[key] {
			skipped++
			continue
		}
		known[key] = true

		if index := pCommon.FactSame(next, fact, hCommon.DistillSimilarFactRatio, hCommon.DistillSimilarFactMinRunes); index >= 0 {
			// 同一槽位的新说法:留下信息更多的那条
			if utf8.RuneCountInString(fact) > utf8.RuneCountInString(next[index]) {
				next[index] = fact
			}
			updated++
			continue
		}
		next = append(next, fact)
	}

	// 与**提示词快照**比对(而不是与文件当前内容比对):哪些被改写(updated)、哪些被模型丢弃(removed)。
	// 并发蒸馏时另一个会话可能已经在本轮开始之后写入了新条目 —— 模型没看过它们,
	// 若按文件当前内容计数,这些新条目会被误判成"被丢弃"而删除。
	for _, old := range req.snapshot {
		index := pCommon.FactSame(next, old, hCommon.DistillSimilarFactRatio, hCommon.DistillSimilarFactMinRunes)
		if index < 0 {
			removed++
			continue
		}
		// 同一槽位的新说法/新取值 = 改写(旧说法已不在最终列表里),与"新增"区分开
		if pCommon.NormalizeFact(next[index]) != pCommon.NormalizeFact(old) {
			updated++
		}
	}

	// 快照之后才出现的条目:模型没机会看到,不能替它裁决 —— 一律保留
	if merged2 := factsAfterSnapshot(oldFacts, req.snapshot, next); len(merged2) > len(next) {
		logging.LogWarnF("chat memory distill: %d facts appeared after the prompt snapshot, keep them (records %d-%d)",
			len(merged2)-len(next), req.fromIndex, req.toIndex)
		next = merged2
	}

	if !legacyOnly && len(req.snapshot) > 0 && removed > len(req.snapshot)/2 {
		// 丢掉一半以上 = 可疑(可能只是模型不听话地"只写了新增") → 降级为追加
		logging.LogWarnF("chat memory distill: model dropped %d/%d existing facts, keep them and append instead (records %d-%d)",
			removed, len(req.snapshot), req.fromIndex, req.toIndex)
		next = appendMissingFacts(oldFacts, next)
		removed = 0
	}

	if len(next) > hCommon.DistillMaxMemoryFacts {
		// 超出条目上限:丢尾部(提示词已要求不超过上限,这里是兜底)
		next = next[:hCommon.DistillMaxMemoryFacts]
	}
	next = fitFactsBudget(next, req.maxChars)

	// 安全网:不能把长期记忆写成"空块" —— 条目被预算裁空时退回旧列表。
	// (迁移期清空是**有意**的,不适用本兜底)
	if len(next) == 0 && len(oldFacts) > 0 && !legacyOnly {
		logging.LogWarnF("chat memory distill: empty fact list after budgeting, keep %d existing facts (records %d-%d)",
			len(oldFacts), req.fromIndex, req.toIndex)
		next = oldFacts
	}

	content := replaceAutoMemory(lines, next, req.sessionId, req.toIndex)
	if err = pCommon.WriteTextAtomically(req.path, content); err != nil {
		return nil, skipped, updated, removed, migrated, err
	}
	return next, skipped, updated, removed, migrated, nil
}

// factsAfterSnapshot 把"本轮开始之后才出现在文件里"的条目补进 next。
//
// 判定依据是条目**不在提示词快照里**:那些条目模型根本没机会看到,
// 因此必须原样保留(并发蒸馏的两个会话各自维护自己的条目,不能互相删)。
func factsAfterSnapshot(current, snapshot, next []string) []string {
	for _, fact := range current {
		if pCommon.FactSame(snapshot, fact, hCommon.DistillSimilarFactRatio, hCommon.DistillSimilarFactMinRunes) >= 0 {
			continue // 快照里有 → 该条由模型裁决
		}
		if pCommon.FactSame(next, fact, hCommon.DistillSimilarFactRatio, hCommon.DistillSimilarFactMinRunes) >= 0 {
			continue
		}
		next = append(next, fact)
	}
	return next
}

// appendMissingFacts 把 base 中"extra 里没有"的条目补到 extra 尾部(降级路径:只加不减)。
func appendMissingFacts(base, extra []string) []string {
	merged := make([]string, 0, len(base)+len(extra))
	merged = append(merged, extra...)
	for _, fact := range base {
		if pCommon.FactSame(merged, fact, hCommon.DistillSimilarFactRatio, hCommon.DistillSimilarFactMinRunes) >= 0 {
			continue
		}
		merged = append(merged, fact)
	}
	return merged
}

// fitFactsBudget 条目总量超出注入预算时从**尾部**丢弃并告警。
// 提示词已要求"总计不超过上限",这里是兜底;丢尾部而不是头部,与"新条目更重要"一致。
func fitFactsBudget(facts []string, maxChars int) []string {
	if maxChars <= 0 {
		return facts
	}

	total := 0
	for i := range facts {
		total += utf8.RuneCountInString(pCommon.ConcatStringWithLineSpace(facts[i])) + 1 // 渲染一条事实行
		if total > maxChars {
			if i == 0 {
				return facts // 单条就超预算时也不能把记忆清空(宁超一点)
			}
			logging.LogWarnF("chat memory distill: memory block exceeds the size budget, dropped %d trailing facts", len(facts)-i)
			return facts[:i]
		}
	}
	return facts
}

// ============================== 自动记忆区(定界/读写) ==============================

// 自动记忆区用一个 HTML 注释定界,便于人工审阅时一眼看出"这块由系统维护":
//
//	<!-- auto-memory:updated=2026-09-15 10:23:45;watermarks=sess-a:120,sess-b:40 -->
//	- 事实…
//	<!-- /auto-memory -->
var distillAutoBeginPattern = regexp.MustCompile(`^<!--\s*auto-memory:updated=([^;]*);watermarks=(.*?)\s*-->$`)

// distillAutoBeginMarkerPrefix 自动区起始注释的前缀(人工审阅与测试用)。
const distillAutoBeginMarkerPrefix = "<!-- auto-memory:"

const distillAutoEndMarker = "<!-- /auto-memory -->"

// existingMemoryFacts 汇总**自动记忆区**里的条目(含迁移期的旧格式小节)与旧格式小节数。
//
// 只取自动区:人工维护区的事实不参与重写 —— 否则模型会把它们再写一份进自动区(重复),
// "删了多少条"的统计也会牵连人工内容。
func existingMemoryFacts(lines []string) (facts []string, legacySections int) {
	start, end, ok := autoRegionBounds(lines)
	if !ok {
		return nil, 0
	}

	for i := start; i < end; i++ {
		if distillSectionHeaderPattern.MatchString(strings.TrimSpace(lines[i])) {
			legacySections++
			continue
		}
		if fact, ok := parseFactLine(lines[i]); ok {
			if pCommon.FactSame(facts, fact, hCommon.DistillSimilarFactRatio, hCommon.DistillSimilarFactMinRunes) < 0 {
				facts = append(facts, fact)
			}
		}
	}
	return facts, legacySections
}

// parseFactLine 解析一条 "- 事实"(非条目行返回 ok=false)。
func parseFactLine(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "- ") {
		return pCommon.BlankString, false
	}
	fact := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
	if fact == pCommon.BlankString {
		return pCommon.BlankString, false
	}
	return fact, true
}

// autoBlockBounds 定位自动记忆区(返回起始/结束注释行的下标)。
func autoBlockBounds(lines []string) (begin, end int, ok bool) {
	begin, end = -1, -1
	for i := range lines {
		trimmed := strings.TrimSpace(lines[i])
		if begin < 0 && distillAutoBeginPattern.MatchString(trimmed) {
			begin = i
			continue
		}
		if begin >= 0 && trimmed == distillAutoEndMarker {
			return begin, i, true
		}
	}
	if begin >= 0 {
		return begin, len(lines), true // 缺结束标记:按"到文件末尾"容错
	}
	return -1, -1, false
}

// replaceAutoMemory 把自动记忆区整体替换为 facts,并把本会话的水印推进到 toIndex。
//
// 替换范围 = "自动区 + 旧格式小节"构成的连续区域(autoRegionBounds):
//   - 只有**时间戳小节/自动区/条目/空行**算区域内容;出现别的内容(手写标题、正文)即视为区域结束,
//     因此人工维护区永远不会被删;
//   - 区域不存在时(全手写文件)把自动区追加到文件末尾。
func replaceAutoMemory(lines []string, facts []string, sessionId string, toIndex int) string {
	marks := map[string]int{}
	if begin, _, ok := autoBlockBounds(lines); ok {
		if m := distillAutoBeginPattern.FindStringSubmatch(strings.TrimSpace(lines[begin])); m != nil {
			marks = parseAutoWatermarks(m[2])
		}
	}
	if toIndex > marks[sessionId] {
		marks[sessionId] = toIndex
	}

	block := renderAutoMemoryBlock(facts, marks, len(lines) > 0)
	start, end, hasRegion := autoRegionBounds(lines)
	if hasRegion {
		// 区域末尾若原本有非空行,替换后补一个空行,保持"小节之间空一行"的排版
		merged := make([]string, 0, len(lines))
		merged = append(merged, lines[:start]...)
		merged = append(merged, block...)
		merged = append(merged, lines[end:]...)
		return pCommon.NormalizeBlankLine(strings.Join(merged, "\n"))
	}

	// 无自动区:接在文件末尾(空一行分隔)
	content := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	if content != pCommon.BlankString {
		content += "\n\n"
	}
	content += strings.Join(block, "\n")
	return pCommon.NormalizeBlankLine(content)
}

// autoRegionBounds 返回"自动内容"在文件里占据的连续区域 [start,end)。
//
// 定位方式:先找**锚点**(自动区定界注释或旧格式时间戳小节标题),再从锚点向后吃自动行
// (空行/时间戳标题/定界注释/条目行),遇到其它内容(手写标题、正文)即停。
//
// 两项保证:
//   - 前导空行**不属于区域**:区域从锚点行开始 —— 这样反复重写不会每次吃掉一个空行
//     (排版能收敛),人工维护区里紧贴自动区的条目行也绝不会被吞进来;
//   - 向后只吃自动行:人工维护区(手写标题/正文)永远不会被删。
//
// 代价是:紧跟在自动区**之后**的裸条目行会被当成自动内容 —— 人工新增内容请写在
// 自动区**之前**的独立小节里(文件头注释里有说明)。
func autoRegionBounds(lines []string) (start, end int, ok bool) {
	anchor := -1
	for i := range lines {
		if isAutoAnchor(lines[i]) {
			anchor = i
			break
		}
	}
	if anchor < 0 {
		return -1, -1, false
	}

	// start 就是锚点行本身:向后不吞前导空行 —— 否则每次重写都会吃掉一个空行,
	// 排版永远收不了(实测:两次重写后人工区与自动区之间的空行会消失)。
	start = anchor
	end = anchor + 1
	for end < len(lines) && isAutoRegionLine(lines[end]) {
		end++
	}
	return start, end, true
}

// isAutoAnchor 区域锚点:自动区定界注释 / 旧格式时间戳小节标题。
func isAutoAnchor(line string) bool {
	trimmed := strings.TrimSpace(line)
	return distillAutoBeginPattern.MatchString(trimmed) || distillSectionHeaderPattern.MatchString(trimmed)
}

// isAutoRegionLine 该行是否属于自动内容(见 autoRegionBounds)。
func isAutoRegionLine(line string) bool {
	trimmed := strings.TrimSpace(line)

	if trimmed == pCommon.BlankString {
		return true
	}
	if distillSectionHeaderPattern.MatchString(trimmed) {
		return true
	}
	if distillAutoBeginPattern.MatchString(trimmed) || trimmed == distillAutoEndMarker {
		return true
	}
	if strings.HasPrefix(trimmed, "<!--") || strings.HasPrefix(trimmed, "-->") {
		return true
	}
	_, isFact := parseFactLine(line)
	return isFact
}

// hasAutoBlock 文件里是否已有自动区定界注释(即:已被"整体重写"设计接管)。
// 没有它 = 区域里只有旧格式时间戳小节 → 处于迁移期。
func hasAutoBlock(lines []string) bool {
	_, _, ok := autoBlockBounds(lines)
	return ok
}

// renderAutoMemoryBlock 渲染自动记忆区(定界注释 + 条目 + 结束标记)。
//
// 条目为空但水位非空时**只渲染定界注释**:迁移期模型明确判定"没有值得长期记住的内容"会把
// 条目清空,但水位必须留着 —— 丢了水位下一轮会把同一段对话重新蒸馏一遍。
func renderAutoMemoryBlock(facts []string, marks map[string]int, useNow bool) []string {
	if len(facts) == 0 && len(marks) == 0 {
		return nil
	}

	now := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime()
	updated := time.Now().Format(hCommon.DistillSectionTimeLayout)
	if useNow && now != nil {
		updated = fmt.Sprintf("%04d-%02d-%02d %02d:%02d:%02d", now.Year, now.Month, now.Day, now.Hour, now.Minute, now.Second)
	}

	block := make([]string, 0, len(facts)+3)
	block = append(block, fmt.Sprintf("<!-- auto-memory:updated=%s;watermarks=%s -->", updated, formatAutoWatermarks(marks)))
	for _, fact := range facts {
		block = append(block, pCommon.ConcatStringWithLineSpace(fact)) //渲染一条事实行
	}
	block = append(block, distillAutoEndMarker)
	return block
}

// parseAutoWatermarks 解析 "sess-a:120,sess-b:40";非法片段忽略(不因此丢整块记忆)。
func parseAutoWatermarks(spec string) map[string]int {
	marks := make(map[string]int)
	for _, item := range strings.Split(spec, ",") {
		item = strings.TrimSpace(item)
		if item == pCommon.BlankString {
			continue
		}
		index := strings.LastIndex(item, ":")
		if index <= 0 {
			continue
		}
		sessionId := strings.TrimSpace(item[:index])
		value := 0
		if _, err := fmt.Sscanf(strings.TrimSpace(item[index+1:]), "%d", &value); err != nil || sessionId == pCommon.BlankString {
			continue
		}
		if value > marks[sessionId] {
			marks[sessionId] = value
		}
	}
	return marks
}

// formatAutoWatermarks 反向渲染水印(按会话号排序,保证同一状态写出同样的文本)。
func formatAutoWatermarks(marks map[string]int) string {
	if len(marks) == 0 {
		return pCommon.BlankString
	}

	keys := make([]string, 0, len(marks))
	for key := range marks {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", key, marks[key]))
	}
	return strings.Join(parts, ",")
}
