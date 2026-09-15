/**
* 记忆记录模型与解析
*
* 职责:把历史记忆文件(memory_*.md)解析回结构化记录,供"上下文读回装配"与"压缩"使用。
*
* 记录格式(与写入端 MemoryFileRWriter.Append 严格对应):
*
*   ## [<index>] <type>[:<title>] | <timeText>
*   <正文>
*
*   - index   记录序号,永久标识(见 memory_file_storage.go 的序号契约:只增不减、压缩不得重排)
*   - type    user / assistant / tool / summary
*   - title   tool 记录为工具名;assistant 记录可为 interrupted(被中断的部分回复)
*   - 正文    summary 记录的首行可带 `covers: <from>-<to>`,标注它取代的记录序号区间
*
* 解析是纯函数(只读、无副作用、不触发 IO),便于单测与复用。
**/

package common

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	pCommon "my.favorite/platformutility/common"
)

// coversPattern 摘要记录的覆盖区间标注:`covers: 1-999`(独占一行)
var coversPattern = regexp.MustCompile(`(?m)^covers:\s*(\d+)\s*-\s*(\d+)\s*$`)

// MemoryRecord 一条历史记录。
type MemoryRecord struct {
	Index      int    // 记录序号(永久标识,只增不减)
	RecordType string // user / assistant / tool / summary
	Title      string // 工具名;或 assistant 的附加标记(interrupted)
	TimeText   string // 记录头时间文本(yyyy-MM-dd HH:mm:ss)
	Content    string // 正文(摘要记录已剔除 covers 行)

	CoversFrom int // 摘要记录覆盖区间起点(闭区间);非摘要为 0
	CoversTo   int // 摘要记录覆盖区间终点(闭区间);非摘要为 0
}

// IsSummary 是否为压缩摘要记录。
func (r MemoryRecord) IsSummary() bool { return r.RecordType == HistoryRecordTypeSummary }

// IsInterrupted 是否为被中断的助手回复(内容为中断时已产出的部分)。
func (r MemoryRecord) IsInterrupted() bool {
	return r.RecordType == HistoryRecordTypeAssistant && r.Title == HistoryRecordTypeInterrupted
}

// Covers 判断本摘要记录是否取代了序号 index 的记录。
func (r MemoryRecord) Covers(index int) bool {
	return r.IsSummary() && r.CoversFrom > 0 && index >= r.CoversFrom && index <= r.CoversTo
}

// recordHeader 解析出的记录头。
type recordHeader struct {
	lineStart  int // 头行在文本中的起始字节偏移
	bodyStart  int // 正文起始字节偏移(头行之后的下一字节)
	index      int
	recordType string
	title      string
	timeText   string
}

// ParseMemoryRecords 解析一段历史记忆文本。
//
// 返回完整记录列表与"已消费字节数":只有"后面还有内容"或"以空行收尾"的记录才算完整,
// 因此增量读取时不会把半条记录(写入中断)解析进来,下次可从 consumed 处继续。
func ParseMemoryRecords(text string) (records []MemoryRecord, consumed int) {
	headers := scanRecordHeaders(text)
	if len(headers) == 0 {
		return nil, 0
	}

	for i, h := range headers {
		bodyEnd := len(text)
		if i+1 < len(headers) {
			bodyEnd = headers[i+1].lineStart
		}
		body := text[h.bodyStart:bodyEnd]

		// 完整性判定:非末条记录天然完整(后面还有头行);末条必须以空行收尾。
		// 不完整的末条(写入中断)不产出,且不计入 consumed,下次增量读取可重新解析。
		if !(i+1 < len(headers) || pCommon.EndsWithBlankLine(body)) {
			break
		}
		consumed = bodyEnd

		rec := MemoryRecord{
			Index:      h.index,
			RecordType: h.recordType,
			Title:      h.title,
			TimeText:   h.timeText,
			Content:    strings.TrimSpace(body),
		}
		if rec.IsSummary() {
			if m := coversPattern.FindStringSubmatch(rec.Content); m != nil {
				rec.CoversFrom, _ = strconv.Atoi(m[1])
				rec.CoversTo, _ = strconv.Atoi(m[2])
				rec.Content = strings.TrimSpace(coversPattern.ReplaceAllString(rec.Content, ""))
			}
		}
		records = append(records, rec)
	}
	return records, consumed
}

// TextRunes 体积记账的**唯一口径**:去掉首尾空白后的 rune 数。
//
// 为什么统一:摘要/事实/源文本的体积都会拿去和同一个阈值(如 MaxSummaryTotalRunes)比较,
// 而注入时的文本本来就是 TrimSpace 过的 —— 两边用不同口径会在阈值附近做出不一致的决定
// (触发判定说"超了"、动作门说"没超")。
func TextRunes(s string) int {
	return utf8.RuneCountInString(strings.TrimSpace(s))
}

// CompactionStat 供日志使用:把记录数/字符数汇总为一行。
func CompactionStat(targets []MemoryRecord) string {
	chars := 0
	for i := range targets {
		chars += TextRunes(targets[i].Content)
	}
	return fmt.Sprintf("records=%d chars=%d", len(targets), chars)
}

// RecordsRunes 汇总一批记录的正文总长(rune),用于压缩的"收益验证"。
func RecordsRunes(records []MemoryRecord) int {
	chars := 0
	for i := range records {
		chars += TextRunes(records[i].Content)
	}
	return chars
}

// RecordsRunesInRange 汇总 [from,to] 区间内记录的正文总长;**一旦达到 limit 就提前返回**。
//
// 用于"值不值得"这类粗判(如被裁区间的体积够不够换一条摘要):
// 只需要知道"够不够",不必为一次判断扫完整段内容(这个判断在请求路径上)。
func RecordsRunesInRange(records []MemoryRecord, from, to, limit int) int {
	chars := 0
	for i := range records {
		if records[i].Index < from || records[i].Index > to {
			continue
		}
		chars += TextRunes(records[i].Content)
		if limit > 0 && chars >= limit {
			return chars
		}
	}
	return chars
}

// CoveredIndexes 汇总摘要记录已经取代的记录序号集合。
func CoveredIndexes(summaries []MemoryRecord) map[int]bool {
	covered := make(map[int]bool)
	for i := range summaries {
		sum := summaries[i]
		for index := sum.CoversFrom; index > 0 && index <= sum.CoversTo; index++ {
			covered[index] = true
		}
	}
	return covered
}

// SummariesOf 取出全部摘要记录(按序号升序;含已被取代的)。
func SummariesOf(records []MemoryRecord) []MemoryRecord {
	var summaries []MemoryRecord
	for i := range records {
		if records[i].IsSummary() {
			summaries = append(summaries, records[i])
		}
	}
	return summaries
}

// PlainRecords 取出全部普通记录(按序号升序;不含摘要记录)。
func PlainRecords(records []MemoryRecord) []MemoryRecord {
	plain := make([]MemoryRecord, 0, len(records))
	for i := range records {
		if !records[i].IsSummary() {
			plain = append(plain, records[i])
		}
	}
	return plain
}

// ActivePlainRecords 取出**仍会回放**的普通记录:既不是摘要记录,也没有被任何摘要代表。
//
// 这是"读取端回放"与"蒸馏输入"共用的口径:被摘要代表的记录既不再回放,
// 也不应再参与蒸馏(否则旧内容会占满蒸馏窗口,新内容永远蒸馏不到)。
func ActivePlainRecords(records []MemoryRecord) []MemoryRecord {
	summaries := SummariesOf(records)
	if len(summaries) == 0 {
		return PlainRecords(records)
	}

	covered := CoveredIndexes(summaries)
	kept := make([]MemoryRecord, 0, len(records))
	for i := range records {
		if records[i].IsSummary() || covered[records[i].Index] {
			continue
		}
		kept = append(kept, records[i])
	}
	return kept
}

// ActiveSummaries 返回**仍然生效**的摘要记录(按序号升序)。
//
// 取代判定:某条更晚的摘要的 covers 区间包含了本条摘要自己的记录序号。
// 正常情况下不会发生 —— 普通压缩的 covers 区间刻意不含任何摘要记录序号(见 mstorage.compactionRun);
// 只有"合并摘要"刻意把被合并的摘要记录序号纳入区间时,才会发生取代。
func ActiveSummaries(records []MemoryRecord) []MemoryRecord {
	summaries := SummariesOf(records)
	if len(summaries) == 0 {
		return nil
	}

	active := make([]MemoryRecord, 0, len(summaries))
	for i := range summaries {
		superseded := false
		for j := i + 1; j < len(summaries); j++ {
			if summaries[j].Covers(summaries[i].Index) {
				superseded = true
				break
			}
		}
		if !superseded {
			active = append(active, summaries[i])
		}
	}
	return active
}

// SummaryTextOf 渲染"生效摘要"注入 system 时的文本(空正文不参与,空行连接)。
//
// 这是摘要文本的**唯一渲染处**:上下文装配(domain 层)与"摘要总量是否超预算"的
// 一切判定/动作门都走它 —— 否则会出现"按 A 算法判要不要合并、按 B 算法算体积"的漂移。
func SummaryTextOf(records []MemoryRecord) string {
	var texts []string
	for _, sum := range ActiveSummaries(records) {
		if content := strings.TrimSpace(sum.Content); content != "" {
			texts = append(texts, content)
		}
	}
	return strings.Join(texts, "\n\n")
}

// SummariesRunes 生效摘要的**注入体积**(rune)—— 与 SummaryTextOf 严格同源。
func SummariesRunes(records []MemoryRecord) int {
	return TextRunes(SummaryTextOf(records))
}

// ============================== private functions & methods ==============================

// scanRecordHeaders 扫描全部记录头(要求 "## [" 处于行首)。
func scanRecordHeaders(text string) []recordHeader {
	var headers []recordHeader

	offset := 0
	for offset < len(text) {
		i := strings.Index(text[offset:], "## [")
		if i < 0 {
			break
		}
		i += offset
		offset = i + 4

		// 必须位于行首
		if i > 0 && text[i-1] != '\n' {
			continue
		}
		lineEnd := strings.IndexByte(text[i:], '\n')
		if lineEnd < 0 {
			break // 头行不完整,等下次增量读取
		}
		lineEnd += i

		h, ok := parseRecordHeader(text[i:lineEnd])
		if !ok {
			continue
		}
		h.lineStart = i
		h.bodyStart = lineEnd + 1
		headers = append(headers, h)
	}
	return headers
}

// parseRecordHeader 解析头行:"## [12] tool:get_weather | 2026-09-13 01:30:00"。
func parseRecordHeader(line string) (recordHeader, bool) {
	if !strings.HasPrefix(line, "## [") {
		return recordHeader{}, false
	}
	closeIdx := strings.IndexByte(line, ']')
	if closeIdx < 0 {
		return recordHeader{}, false
	}

	index, err := strconv.Atoi(strings.TrimSpace(line[4:closeIdx]))
	if err != nil || index <= 0 {
		return recordHeader{}, false
	}

	rest := strings.TrimSpace(line[closeIdx+1:])
	timeText := ""
	if p := strings.LastIndex(rest, " | "); p >= 0 {
		timeText = strings.TrimSpace(rest[p+3:])
		rest = strings.TrimSpace(rest[:p])
	}

	recordType, title := rest, ""
	if p := strings.IndexByte(rest, ':'); p >= 0 {
		recordType = strings.TrimSpace(rest[:p])
		title = strings.TrimSpace(rest[p+1:])
	}
	if recordType == "" {
		return recordHeader{}, false
	}
	return recordHeader{index: index, recordType: recordType, title: title, timeText: timeText}, true
}
