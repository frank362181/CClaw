/**
* 历史记忆的压缩(compact)
*
* 目标:会话越聊越长,历史不可能无限回放。压缩把"一整段旧记录"浓缩为一条**摘要记录**,
* 从而让历史体积有界,同时不丢失早期信息(见 memory_file_storage.go 顶部的序号契约)。
*
* 关键设计(为什么这样做):
*  1. **不阻塞用户回复**。压缩由调用方在回复产出后异步触发,失败只记日志:
*     记忆是增强能力,不能成为聊天链路的故障点。
*  2. **幂等**。压缩前先确认目标区间尚未被已有摘要覆盖;同一会话的压缩串行化(专用锁键)。
**/

package ctxmgr

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	hCommon "my.favorite/cclawservice/harness/common"
	pCommon "my.favorite/platformutility/common"
)

// CompactionResult 压缩结果。
type CompactionResult struct {
	SummaryIndex int    // 摘要记录的序号(0 表示未产生摘要)
	CoversFrom   int    // 摘要覆盖区间起点
	CoversTo     int    // 摘要覆盖区间终点
	Summary      string // 摘要正文(便于日志/测试观测)
	Mode         int    // 实际使用的模式
	Degraded     bool   // 是否因保全校验失败而降级(缩小了覆盖区间)
	Merged       int    // 合并摘要时被取代的摘要条数(0 表示本次不是合并)
}

// buildMergeUserPrompt 渲染"合并摘要"的输入(输入是摘要正文,不是原始记录)。
func buildMergeUserPrompt(set []MemoryRecord, from, to int) string {
	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, CompactionMergeUserHeader, from, to)
	for i := range set {
		_, _ = fmt.Fprintf(&sb, "[%d] (covers %d-%d): %s\n", set[i].Index, set[i].CoversFrom, set[i].CoversTo,
			pCommon.TruncateRunes(strings.TrimSpace(set[i].Content), MemoryExcerptRunes, TruncatedMarker))
	}
	return sb.String()
}

// recordsInRange 取出 [from,to] 内的记录(升序)。
func recordsInRange(records []MemoryRecord, from, to int) []MemoryRecord {
	var in []MemoryRecord
	for i := range records {
		if records[i].Index >= from && records[i].Index <= to {
			in = append(in, records[i])
		}
	}
	return in
}

// compactionRun 在 [from,to] 内挑出**一段可安全压缩的连续普通记录**。
//
// 为什么必须"连续"且"不含摘要记录"(P0:covers 的精确性):
// 摘要落盘时用 `covers: A-B` 表达"这段历史已由本摘要代表",读取端据此让区间内的记录永久退出回放。
// 这个承诺只有在"区间内每条记录都真的送进过模型"时才成立,因此屏障只有三种:
//   - 摘要记录本身:区间不能跨过它 —— 否则后续的"被更晚摘要取代"判定会把那条早期摘要
//     误判为已被取代,而新摘要里并没有它的内容(信息静默丢失);
//   - 已被摘要代表的记录:它已由摘要代表,不是本次任务;
//   - 序号不连续:区间中间有空洞,无法用单区间表达。
//
// 为什么要"挑"而不是"从头取第一段":屏障前面的残留段可能不足 CompactionMinRecords
// (例如摘要序号紧跟在 1~3 条新记录之后 —— 这是常态,因为摘要序号是 LLM 调用结束后才分配的),
// 从头取会让屏障后面的记录**永远够不到**,压缩就此停摆。
// 因此这里在所有候选段里挑一段:优先"条数达标且体积最大"的(体积最大 = 最可能通过收益门槛),
// 没有达标的段时返回体积最大的那段(交由调用方按门槛拒绝)。
func compactionRun(records []MemoryRecord, from, to int) []MemoryRecord {
	covered := CoveredIndexes(SummariesOf(records))

	var best, bestUsable, current []MemoryRecord
	flush := func() {
		if len(current) == 0 {
			return
		}
		if volume(best) < volume(current) {
			best = current
		}
		if len(current) >= hCommon.CompactionMinRecords && volume(bestUsable) < volume(current) {
			bestUsable = current
		}
		current = nil
	}

	for i := range records {
		rec := records[i]
		if rec.Index < from || rec.Index > to {
			continue
		}
		blocked := rec.IsSummary() || covered[rec.Index] ||
			(len(current) > 0 && rec.Index != current[len(current)-1].Index+1)
		if blocked {
			flush()
			continue
		}
		current = append(current, rec)
	}
	flush()

	if len(bestUsable) > 0 {
		return bestUsable
	}
	return best
}

// volume 一段记录的正文总长(rune),用于挑选"最值得压缩"的段。
func volume(records []MemoryRecord) int {
	return RecordsRunes(records)
}

// fitCompactionRange 按"送入摘要模型的字符预算"收缩区间**尾部**。
//
// budgetRunes 是**源文本**预算(rune):由调用方按模型窗口推导
// (见 ctxmgr.CalcMaintenanceBudgets);窗口未知时等于 hCommon.MaxCompactionInputChars。
//
// 关键:宁可不压,也不能"丢弃区间内的记录却宣称覆盖整个区间" ——
// covers 是承诺,区间内的每条记录都必须真的送进模型,否则那部分原文会永久退出回放。
// 因此放不下时收缩 coverTo(剩下的部分下次再压),而不是把记录丢在区间里。
// 返回收缩后的记录集与其长度;记录集至少保留 1 条。
func fitCompactionRange(run []MemoryRecord, budgetRunes int) ([]MemoryRecord, int) {
	if len(run) == 0 {
		return nil, 0
	}

	total := 0
	fit := 0
	for i := range run {
		lineRunes := utf8.RuneCountInString(compactionPromptLine(run[i]))
		if i > 0 && total+lineRunes > budgetRunes {
			break
		}
		total += lineRunes
		fit = i + 1
	}
	return run[:fit], fit
}

// coverChecker 预建的"序号 → 记录"索引。
// verifyCovers 只需遍历 covers 区间,而不是全量记录:
// mergeCandidate 会对每个候选前缀各校验一次,用索引可把复杂度从 O(记录数 × 摘要数)
// 降到 O(区间长度 × 摘要数)。
type coverChecker map[int]MemoryRecord

// newCoverChecker 为一批记录建索引(建一次可反复校验)。
func newCoverChecker(records []MemoryRecord) coverChecker {
	checker := make(coverChecker, len(records))
	for i := range records {
		checker[records[i].Index] = records[i]
	}
	return checker
}

// verify 校验 covers 区间 [from,to] 与"真正送进模型的记录集合"是否一致。
// 见 verifyCovers 的规则说明。
func (c coverChecker) verify(from, to int, consumed, covered map[int]bool) error {
	for index := from; index <= to; index++ {
		rec, ok := c[index]
		if !ok {
			continue // 该序号没有记录(空洞):没有"未经摘要的原文"可言
		}
		if consumed[index] || covered[index] {
			continue
		}

		kind := "记录"
		if rec.IsSummary() {
			kind = "摘要记录"
		}
		return fmt.Errorf("covers %d-%d over-claims: %s #%d was never sent to the model", from, to, kind, index)
	}
	return nil
}

// verifyCovers 校验"摘要声明的 covers 区间"与"真正送进模型的记录集合"一致。
//
// 规则:区间 [from,to] 内的每条记录必须满足其一 ——
//  1. 属于本次送入模型的集合(consumed);
//  2. 已被**既有摘要**覆盖(它本来就已退出回放,不因本条摘要而改变)。
//
// 不满足时必须放弃落盘:过度承诺会让未经摘要的原文永久退出回放(信息静默丢失)。
func verifyCovers(records []MemoryRecord, from, to int, consumed, covered map[int]bool) error {
	return newCoverChecker(records).verify(from, to, consumed, covered)
}

// effectiveSummaryCap 按源体积推导摘要长度上限。
// 压缩的价值来自"变小":若摘要上限仍不小于源体积,压完只会更占预算(而摘要折进 system,
// system 越长历史预算越小 —— 会形成正反馈)。源太小时调用方直接跳过压缩。
func effectiveSummaryCap(srcRunes int) int {
	limit := int(float64(srcRunes) * hCommon.SummaryShrinkRatio)
	if limit < hCommon.SummaryMinRunes {
		limit = hCommon.SummaryMinRunes
	}
	if limit > hCommon.MaxSummaryRunes {
		limit = hCommon.MaxSummaryRunes
	}
	return limit
}

// mergeCandidate 选出可以安全合并的摘要集合(从最旧开始的**前缀**),返回集合与 covers 区间。
//
// 合并产生的 covers 区间 [A,B]:
//
//	A = 集合中最旧摘要的 CoversFrom
//	B = max(集合中最大 CoversTo, 集合中最大摘要记录序号)  ← 必须含摘要序号才能取代它们
//
// 随后用 verifyCovers 校验:区间内每条普通记录必须已被既有摘要覆盖,
// 每条摘要记录必须属于本集合(否则它不会被取代,内容会重复注入)。
// 从最大前缀向下尝试;找不到(或不足 2 条)返回 nil ——
// 宁可摘要略超预算,也不写一条"过度承诺"的摘要(那会永久丢历史)。
func mergeCandidate(records []MemoryRecord) ([]MemoryRecord, int, int) {
	return mergeCandidateUpTo(records, 0)
}

// mergeCandidateUpTo 同 mergeCandidate,但把候选集合限制在至多 maxSize 条最旧摘要
// (maxSize <= 0 表示不限制)。
//
// 为什么要能限制:合并是"一次处理越多越好"与"一次处理越少越不容易丢关键标记"之间的取舍。
// 保留率不达标时退回到最小集合再试一次,而不是直接卡死(见 mergeWithBudget 的重试)。
func mergeCandidateUpTo(records []MemoryRecord, maxSize int) ([]MemoryRecord, int, int) {
	summaries := SummariesOf(records)
	if len(summaries) < hCommon.CompactionMergeMinSummaries {
		return nil, 0, 0
	}

	limit := len(summaries)
	if maxSize > 0 && maxSize < limit {
		limit = maxSize
	}

	covered := CoveredIndexes(summaries)
	checker := newCoverChecker(records)
	for size := limit; size >= hCommon.CompactionMergeMinSummaries; size-- {
		set := summaries[:size]
		if set[0].CoversFrom <= 0 {
			continue
		}

		from, to := set[0].CoversFrom, set[0].CoversTo
		consumed := make(map[int]bool, size)
		inSet := make(map[int]bool, size)
		for i := range set {
			inSet[set[i].Index] = true
			consumed[set[i].Index] = true
			if set[i].CoversTo > to {
				to = set[i].CoversTo
			}
			if set[i].Index > to {
				to = set[i].Index
			}
		}

		// 区间内若存在"不属于本集合的摘要记录",它不会被取代 → 需要换更大的集合
		complete := true
		for index := from; index <= to; index++ {
			if rec, ok := checker[index]; ok && rec.IsSummary() && !inSet[index] {
				complete = false
				break
			}
		}
		if !complete {
			continue
		}
		if err := checker.verify(from, to, consumed, covered); err != nil {
			continue
		}
		return set, from, to
	}
	return nil, 0, 0
}

// summarizeOnce —— default 模式:整段区间一次摘要。
func summarizeOnce(ctx context.Context, targets []MemoryRecord, from, to int, summarize Summarizer) (string, error) {
	return summarize(ctx, CompactionSystemPrompt, buildCompactionUserPrompt(targets, from, to))
}

// summarizeInBlocks —— safeguard 模式:分块摘要后拼接。
// 块内条目少,模型更不容易在长输入里丢要点(代价:调用次数多、摘要更长)。
func summarizeInBlocks(ctx context.Context, targets []MemoryRecord, summarize Summarizer) (string, error) {
	if len(targets) <= SafeguardBlockRecords {
		chunk := targets
		return summarize(ctx, CompactionSystemPrompt, buildCompactionUserPrompt(chunk, chunk[0].Index, chunk[len(chunk)-1].Index))
	}

	var blocks []string
	for start := 0; start < len(targets); start += SafeguardBlockRecords {
		end := start + SafeguardBlockRecords
		if end > len(targets) {
			end = len(targets)
		}
		chunk := targets[start:end]
		text, err := summarize(ctx, CompactionSystemPrompt, buildCompactionUserPrompt(chunk, chunk[0].Index, chunk[len(chunk)-1].Index))
		if err != nil {
			return pCommon.BlankString, err
		}
		if text = strings.TrimSpace(text); text != pCommon.BlankString {
			blocks = append(blocks, text)
		}
	}
	return strings.Join(blocks, "\n"), nil
}

// compactionPromptLine 渲染一条记录送入摘要模型的文本(逐条限长)。
func compactionPromptLine(rec MemoryRecord) string {
	body := pCommon.TruncateRunes(strings.TrimSpace(rec.Content), MemoryExcerptRunes, TruncatedMarker)
	return fmt.Sprintf("[%d] %s: %s\n", rec.Index, recordLabel(rec), body)
}

// promptLineRunes 一条记录送审时的**行体积**(rune)。
//
// 为什么需要它:体积判定(蒸馏压力)与体积消耗(选批/收缩区间)必须是**同一个口径** ——
// 否则会出现"压力说攒够一轮了"而实际批次只装了零头(或反过来)。
// 它与 compactionPromptLine 渲染出来的行严格一致,但**不构造字符串**:
// 压力扫描在请求路径上,而单条工具记录可能上百 KB(构造会白花内存与 GC)。
// 公式与渲染是同一个来源,一致性由 TestPromptLineRunesMatchesRendered 锁住 ——
// 改渲染格式必须同步改这里。
func promptLineRunes(rec MemoryRecord) int {
	body := TextRunes(rec.Content)
	if body > MemoryExcerptRunes {
		// 截断后的正文 = 上限 + 截断标记(与 pCommon.TruncateRunes 的行为一致)
		body = MemoryExcerptRunes + utf8.RuneCountInString(TruncatedMarker)
	}

	return len("[") + len(strconv.Itoa(rec.Index)) + len("] ") +
		utf8.RuneCountInString(recordLabel(rec)) + len(": ") + body + len("\n")
}

// buildCompactionUserPrompt 渲染送入模型的源文本。
//
// records 必须已经是 compactionRun + fitCompactionRange 之后的**连续**区间:
// 这里**不再丢弃任何记录** —— 丢弃记录会让 covers 变成对"未经摘要的原文"的过度承诺。
func buildCompactionUserPrompt(records []MemoryRecord, from, to int) string {
	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, CompactionUserHeader, from, to)
	for i := range records {
		sb.WriteString(compactionPromptLine(records[i]))
	}
	return sb.String()
}

// recordLabel 记录的角色标签。
func recordLabel(rec MemoryRecord) string {
	switch rec.RecordType {
	case HistoryRecordTypeTool:
		if name := strings.TrimSpace(rec.Title); name != "" {
			return "tool:" + name
		}
		return "tool"
	case HistoryRecordTypeAssistant:
		if rec.IsInterrupted() {
			return "assistant(被中断)"
		}
		return "assistant"
	default:
		return rec.RecordType
	}
}

// buildSummaryRecordContent 构造摘要记录正文:首行 covers 标注,其后为摘要正文。
// covers 行是机器可读的契约(读取端据此跳过被取代的记录),必须固定格式。
//
// 正文的**主上限**由调用方按源体积推导(effectiveSummaryCap),这里按 MaxSummaryRunes
// 再截断一次只是幂等兜底(保证"校验过的文本"与"落盘的文本"永远一致);
// 调大 MaxSummaryRunes 不会放大实际摘要 —— 实际上限看 effectiveSummaryCap。
func buildSummaryRecordContent(from, to int, summary string) string {
	return fmt.Sprintf("covers: %d-%d\n\n%s", from, to, pCommon.TruncateRunes(summary, hCommon.MaxSummaryRunes, TruncatedMarker))
}

// 【safeguard 专属】keyRetentionRatio 计算"关键信息保有率":源记录中出现的关键标记(path/编号/文件名)
// 有多少同样出现在摘要里。返回保有率与未命中的样例(供日志排查)。
func keyRetentionRatio(records []MemoryRecord, summary string) (float64, []string) {
	seen := make(map[string]bool)
	var keys []string
	for _, rec := range records {
		for _, key := range KeyTokenPattern.FindAllString(rec.Content, -1) {
			if seen[key] {
				continue
			}
			seen[key] = true
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return 1, nil
	}

	hit := 0
	var missed []string
	for _, key := range keys {
		if strings.Contains(summary, key) {
			hit++
			continue
		}
		if len(missed) < 8 {
			missed = append(missed, key)
		}
	}
	return float64(hit) / float64(len(keys)), missed
}

// 【safeguard 专属】degradeCoverTo 保全校验失败时的降级:只覆盖较老的一半,让较新的原文继续可回放。
//
// 已知且可接受的副作用:摘要正文是按**整段** run 生成的,降级后只声明覆盖前半,
// 于是后半的内容会"摘要里有、原文也回放"一次(重复占预算,方向安全 —— 不会丢信息)。
// 要消除它需要按降级后的区间重新摘要(多一次 LLM 调用),当前不做。
func degradeCoverTo(targets []MemoryRecord, from, to int) int {
	if len(targets) < 2 {
		return to
	}
	mid := len(targets) / 2
	if mid <= 0 {
		return to
	}
	candidate := targets[mid-1].Index
	if candidate < from {
		return to
	}
	return candidate
}
