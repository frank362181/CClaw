/**
* 历史记忆的压缩(compact)
*
* 目标:会话越聊越长,历史不可能无限回放。压缩把"一整段旧记录"浓缩为一条**摘要记录**,
* 从而让历史体积有界,同时不丢失早期信息(见 memory_file_storage.go 顶部的序号契约)。
*
* 关键设计(为什么这样做):
*  1. **只追加、不重排**。摘要作为一条**序号更大**的新记录追加到同一个历史文件末尾,
*     并用自己的 covers 行标注它取代的记录区间。绝不删除/重排原记录 ——
*     t_turn.chat_index / resp_index 保存的是历史序号,一旦回退就会索引错位。
*  2. **不需要新表**。t_session/t_turn/t_turn_tool 三表结构保持不变,摘要只活在历史文件里;
*     读取端(ctxmgr.AssembleContext)按 covers 跳过被取代的记录。
*  3. **不阻塞用户回复**。压缩由调用方在回复产出后异步触发,失败只记日志:
*     记忆是增强能力,不能成为聊天链路的故障点。
*  4. **两种模式**(t_agent.compaction_mode,由调用方传入,本文件内只影响下面三处):
*  5. **幂等**。压缩前先确认目标区间尚未被已有摘要覆盖;同一会话的压缩串行化(专用锁键)。
**/

package mstorage

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	hCommon "my.favorite/cclawservice/harness/common"
	pCommon "my.favorite/platformutility/common"
)

// Summarizer 摘要生成器:由调用方注入(service 层包装 LLM 客户端)。
// 用函数类型而非接口,是为了让 mstorage 不反向依赖 harness(否则会形成 import cycle)。
type Summarizer func(ctx context.Context, systemPrompt, userPrompt string) (string, error)

// 压缩提示词
const (
	compactionSystemPrompt = "你是对话历史压缩器。你的任务是把给定的历史片段压缩为简明摘要,供后续对话继续使用。\n" +
		"要求:\n" +
		"1. 保留事实性信息:用户的目标与偏好、已确认的结论、关键数据(数字、编号、日期)、" +
		"文件路径与命令、尚未完成的待办事项及其状态;\n" +
		"2. 删除寒暄、重复表述与无关细节;\n" +
		"3. 只依据给定内容,不得推测或补造任何信息;不确定的内容标注为“未确认”;\n" +
		"4. 用第三人称陈述,按时间顺序分条列出,不要输出标题之外的多余格式;\n" +
		"5. 若片段中有工具调用结果,只保留结论性的数据,不要复述原始输出全文。"

	compactionUserHeader = "请压缩下面这段历史(记录序号 %d-%d):\n\n"
)

// keyTokenPattern 关键信息采样:文件路径、长数字(编号/金额/日期)、带扩展名的文件名。
// 【safeguard 专属】仅用于保全校验(判断摘要是否丢掉了"不能丢"的信息)。
var keyTokenPattern = regexp.MustCompile(`(?:[A-Za-z]:)?(?:[\w.\-]+/)+[\w.\-]+|\b\d{4,}\b|\b[\w\-]{2,}\.(?:go|json|md|ini|sql|txt|log|yaml|yml|sh|bat)\b`)

// CompactionResult 压缩结果。
type CompactionResult struct {
	SummaryIndex int    // 摘要记录的序号(0 表示未产生摘要)
	CoversFrom   int    // 摘要覆盖区间起点
	CoversTo     int    // 摘要覆盖区间终点
	Summary      string // 摘要正文(便于日志/测试观测)
	Mode         int    // 实际使用的模式
	Degraded     bool   // 是否因保全校验失败而降级(缩小了覆盖区间)
}

// ============================== private functions & methods ==============================

// compactionTargets 取出区间内尚未被摘要覆盖的普通记录(按序号升序)。
func compactionTargets(records []hCommon.MemoryRecord, from, to int) []hCommon.MemoryRecord {
	var covered []hCommon.MemoryRecord
	for _, rec := range records {
		if rec.IsSummary() {
			covered = append(covered, rec)
		}
	}

	targets := make([]hCommon.MemoryRecord, 0, to-from+1)
	for _, rec := range records {
		if rec.IsSummary() || rec.Index < from || rec.Index > to {
			continue
		}
		superseded := false
		for _, sum := range covered {
			if sum.Covers(rec.Index) {
				superseded = true
				break
			}
		}
		if !superseded {
			targets = append(targets, rec)
		}
	}
	return targets
}

// summarizeOnce —— default 模式:整段区间一次摘要。
func summarizeOnce(ctx context.Context, targets []hCommon.MemoryRecord, from, to int, summarize Summarizer) (string, error) {
	return summarize(ctx, compactionSystemPrompt, buildCompactionUserPrompt(targets, from, to))
}

// summarizeInBlocks —— safeguard 模式:分块摘要后拼接。
// 块内条目少,模型更不容易在长输入里丢要点(代价:调用次数多、摘要更长)。
func summarizeInBlocks(ctx context.Context, targets []hCommon.MemoryRecord, summarize Summarizer) (string, error) {
	if len(targets) <= hCommon.SafeguardBlockRecords {
		chunk := targets
		return summarize(ctx, compactionSystemPrompt, buildCompactionUserPrompt(chunk, chunk[0].Index, chunk[len(chunk)-1].Index))
	}

	var blocks []string
	for start := 0; start < len(targets); start += hCommon.SafeguardBlockRecords {
		end := start + hCommon.SafeguardBlockRecords
		if end > len(targets) {
			end = len(targets)
		}
		chunk := targets[start:end]
		text, err := summarize(ctx, compactionSystemPrompt, buildCompactionUserPrompt(chunk, chunk[0].Index, chunk[len(chunk)-1].Index))
		if err != nil {
			return pCommon.BlankString, err
		}
		if text = strings.TrimSpace(text); text != pCommon.BlankString {
			blocks = append(blocks, text)
		}
	}
	return strings.Join(blocks, "\n"), nil
}

// buildCompactionUserPrompt 渲染送入模型的源文本(逐条限长 + 总量上限)。
func buildCompactionUserPrompt(records []hCommon.MemoryRecord, from, to int) string {
	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, compactionUserHeader, from, to)

	total := 0
	for _, rec := range records {
		body := pCommon.TruncateRunes(strings.TrimSpace(rec.Content), hCommon.CompactionRecordExcerptRunes, hCommon.TruncatedMarker)
		line := fmt.Sprintf("[%d] %s: %s\n", rec.Index, recordLabel(rec), body)

		// 超预算则停止追加:装配/压缩都以"序号小的先丢"为原则,这里同样优先保留较新的内容
		if total+utf8.RuneCountInString(line) > hCommon.MaxCompactionInputChars {
			sb.WriteString("\n(更早的内容已略去)\n")
			break
		}
		sb.WriteString(line)
		total += utf8.RuneCountInString(line)
	}
	return sb.String()
}

// recordLabel 记录的角色标签。
func recordLabel(rec hCommon.MemoryRecord) string {
	switch rec.RecordType {
	case hCommon.HistoryRecordTypeTool: //HistoryRecordTool:
		if name := strings.TrimSpace(rec.Title); name != "" {
			return "tool:" + name
		}
		return "tool"
	case hCommon.HistoryRecordTypeAssistant: //HistoryRecordAssistant:
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
// 正文按 MaxSummaryRunes 截断:调用方(Compact)**已先截断再做保全校验**,
// 此处是幂等的兜底,确保"校验过的文本"与"落盘的文本"始终一致。
func buildSummaryRecordContent(from, to int, summary string) string {
	return fmt.Sprintf("covers: %d-%d\n\n%s", from, to, pCommon.TruncateRunes(summary, hCommon.MaxSummaryRunes, hCommon.TruncatedMarker))
}

// 【safeguard 专属】keyRetentionRatio 计算"关键信息保有率":源记录中出现的关键标记(path/编号/文件名)
// 有多少同样出现在摘要里。返回保有率与未命中的样例(供日志排查)。
func keyRetentionRatio(records []hCommon.MemoryRecord, summary string) (float64, []string) {
	seen := make(map[string]bool)
	var keys []string
	for _, rec := range records {
		for _, key := range keyTokenPattern.FindAllString(rec.Content, -1) {
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
func degradeCoverTo(targets []hCommon.MemoryRecord, from, to int) int {
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
