/**
* 记忆蒸馏(把会话里的"长期有效事实"写入 Agent 工作区的 MEMORY.md)
*
* 与压缩的区别:
*   - 压缩(compaction.go)面向**单个会话**:让本会话历史体积有界,摘要留在会话历史文件内;
*   - 蒸馏(distillation.go)面向**Agent 的长期记忆**:把跨会话仍然有效的事实沉淀到工作区文件,
*     由 prompt 组装层按固定顺序注入 system prompt(AGENTS → SOUL → ... → MEMORY)。
*
* 设计要点:
*  1. **落点是文件不是表**:MEMORY.md 已是既有的注入入口(存在才注入),无需新增表/列,
*     也不改变 t_session/t_turn/t_turn_tool 三表结构;
*  2. **只追加、不重写历史**:写成带时间戳的独立小节,便于人工审阅与回溯;
*  3. **去重**:与文件中已有条目做规范化比对,重复事实不再写入;
*  4. **文件有界**:超过注入预算时从**最旧的小节**开始裁剪(预算被最新内容优先占用);
*  5. **原子写入**:先写临时文件再改名,避免进程中断留下半截文件(该文件会被注入 system prompt);
*  6. **失败静默**:蒸馏属于旁路增强,任何错误只记日志,绝不影响对话。
**/

package mstorage

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	cCommon "my.favorite/cclawservice/common"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

// 蒸馏提示词
const (
	distillSystemPrompt = "你负责从对话中提炼**长期有效**的记忆,写入智能体的长期记忆文件。\n" +
		"只提炼跨会话仍然成立、将来还会用到的信息,例如:\n" +
		"- 用户的偏好与习惯(称呼、语言、输出格式偏好、禁忌);\n" +
		"- 稳定的事实与约定(项目结构、路径、账号/单位信息、命名规范);\n" +
		"- 已确认的结论与决策(为什么这么做、被否决的方案);\n" +
		"- 尚未完成的长期待办。\n" +
		"输出要求:\n" +
		"1. 每条一行,以 \"- \" 开头,一句话说清,不写理由过程;\n" +
		"2. 不要记录一次性的内容(本次问答、临时计算结果、寒暄);\n" +
		"3. 不得推测或补造,只依据给定对话;\n" +
		"4. 没有任何值得长期记住的内容时,只输出 " + hCommon.DistillNoneMark + "。"

	distillUserHeader = "以下是一段对话记录(记录序号 %d-%d),请提炼长期记忆:\n\n"
)

// distillSectionHeaderPattern 匹配小节标题 "## 2026-09-13 01:02:03 | session xxx"
var distillSectionHeaderPattern = regexp.MustCompile(`(?m)^## \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}.*$`)

// DistillResult 蒸馏结果。
type DistillResult struct {
	Path    string   // MEMORY.md 路径(未写入时为空)
	Facts   []string // 本次真正写入的事实
	Skipped int      // 因重复而跳过的条数
	Pruned  int      // 因超预算被裁剪掉的旧小节数
}

// memoryDocPath 返回某 Agent 的长期记忆文件路径
func memoryDocPath(agent dataobject.TAgent) (string, bool) {
	root, ok := cCommon.BuildAgentBootstrapRoot(agent)
	if !ok {
		return pCommon.BlankString, false
	}
	return filepath.Join(root, hCommon.MemoryDocFileName), true
}

// plainRecords 过滤掉摘要记录(摘要是压缩产物,不是原始对话事实)。
func plainRecords(records []hCommon.MemoryRecord) []hCommon.MemoryRecord {
	kept := make([]hCommon.MemoryRecord, 0, len(records))
	for _, rec := range records {
		if !rec.IsSummary() {
			kept = append(kept, rec)
		}
	}
	return kept
}

// buildDistillUserPrompt 渲染蒸馏的输入(逐条限长 + 总量上限)。
func buildDistillUserPrompt(records []hCommon.MemoryRecord) string {
	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, distillUserHeader, records[0].Index, records[len(records)-1].Index)

	total := 0
	for _, rec := range records {
		body := pCommon.TruncateRunes(strings.TrimSpace(rec.Content), hCommon.CompactionRecordExcerptRunes, hCommon.TruncatedMarker)
		line := fmt.Sprintf("[%d] %s: %s\n", rec.Index, recordLabel(rec), body)
		if total+utf8.RuneCountInString(line) > hCommon.DistillMaxInputChars {
			sb.WriteString("\n(更早的内容已略去)\n")
			break
		}
		sb.WriteString(line)
		total += utf8.RuneCountInString(line)
	}
	return sb.String()
}

// parseDistilledFacts 解析模型输出:优先取 "- " 开头的行;
// 若模型完全没用列表(一条都没有),则回退为"非空、非标题、非 NONE"的行,兼容不听话的输出。
func parseDistilledFacts(text string) []string {
	text = strings.TrimSpace(text)
	if text == pCommon.BlankString || strings.EqualFold(text, hCommon.DistillNoneMark) {
		return nil
	}

	var bulleted, plain []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == pCommon.BlankString || strings.EqualFold(line, hCommon.DistillNoneMark) || strings.HasPrefix(line, "#") {
			continue
		}

		isBullet := strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "• ")
		item := strings.TrimSpace(strings.TrimLeft(line, "-*•"))
		if item == pCommon.BlankString {
			continue
		}
		if isBullet {
			bulleted = append(bulleted, item)
		} else {
			plain = append(plain, item)
		}
	}

	items := bulleted
	if len(items) == 0 {
		items = plain
	}

	facts := make([]string, 0, len(items))
	for _, item := range items {
		facts = append(facts, pCommon.TruncateRunes(item, hCommon.DistillMaxFactRunes, hCommon.TruncatedMarker))
		if len(facts) >= hCommon.DistillMaxFacts {
			break
		}
	}
	return facts
}

// appendFactsToMemoryDoc 把事实追加为新的时间戳小节;返回本次写入的事实、重复跳过数、被裁剪的旧小节数。
//
// 并发安全:按"文件路径"加锁(与历史文件锁键区分),持锁期间完成"读-比对-写"整个流程;
// 写入采用"临时文件 + 改名",保证注入端永远读到完整文件。
func appendFactsToMemoryDoc(path string, facts []string, sessionId string, maxChars int) (written []string, skipped, pruned int, err error) {
	unlock := GetStorageLocks().Lock(hCommon.BuildMemoryDocLockKey(path))
	defer unlock()

	existing, readErr := pCommon.ReadTextIfExists(path)
	if readErr != nil {
		return nil, 0, 0, readErr
	}

	known := memoryDocKnownFacts(existing)
	var fresh []string
	for _, fact := range facts {
		key := pCommon.NormalizeFact(fact)
		if key == pCommon.BlankString || known[key] {
			skipped++
			continue
		}
		known[key] = true
		fresh = append(fresh, fact)
	}
	if len(fresh) == 0 {
		return nil, skipped, 0, nil
	}

	now := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime()
	section := buildDistillSection(now, sessionId, fresh)

	content := strings.TrimRight(existing, "\n")
	if content != pCommon.BlankString {
		content += "\n\n"
	}
	content += section

	content, pruned = pruneMemoryDoc(content, maxChars)
	if err = pCommon.WriteTextAtomically(path, content); err != nil {
		return nil, skipped, pruned, err
	}
	return fresh, skipped, pruned, nil
}

// buildDistillSection 构造一个小节:"## <时间> | session <id>" + 条目行。
func buildDistillSection(now *pCommon.DatetimeItem, sessionId string, facts []string) string {
	timeText := time.Now().Format(hCommon.DistillSectionTimeLayout)
	if now != nil {
		timeText = fmt.Sprintf("%04d-%02d-%02d %02d:%02d:%02d", now.Year, now.Month, now.Day, now.Hour, now.Minute, now.Second)
	}

	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "## %s | session %s\n\n", timeText, sessionId)
	for _, fact := range facts {
		sb.WriteString("- ")
		sb.WriteString(fact)
		sb.WriteString("\n")
	}
	return sb.String()
}

// memoryDocKnownFacts 建立"已存在事实"的规范化索引(用于去重)。
func memoryDocKnownFacts(content string) map[string]bool {
	known := make(map[string]bool)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
		if key := pCommon.NormalizeFact(line); key != pCommon.BlankString {
			known[key] = true
		}
	}
	return known
}

// pruneMemoryDoc 在超出字符预算时从**最旧的小节**开始裁剪;返回新内容与被裁剪的小节数。
// 最新小节永远保留(否则新记忆刚写入就被砍掉)。
func pruneMemoryDoc(content string, maxChars int) (string, int) {
	if maxChars <= 0 || utf8.RuneCountInString(content) <= maxChars {
		return content, 0
	}

	pruned := 0
	for {
		// 每次基于**当前**内容重新定位小节:裁剪后偏移会整体前移,
		// 沿用旧偏移会切在错误的位置(会切碎条目)。
		locs := distillSectionHeaderPattern.FindAllStringIndex(content, -1)
		if len(locs) <= 1 { // 只剩一个小节:不再裁剪(宁可超出预算,也不丢最新记忆)
			return content, pruned
		}

		// 前置说明文字(首次创建时可能存在)始终保留
		candidate := content[:locs[0][0]] + content[locs[1][0]:]
		if utf8.RuneCountInString(candidate) <= maxChars {
			return candidate, pruned
		}
		content = candidate
		pruned++
	}
}
