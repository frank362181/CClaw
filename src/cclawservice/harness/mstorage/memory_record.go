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

package mstorage

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	pCommon "my.favorite/platformutility/common"
)

// 记录类型 / 标记(与写入端常量保持一致,此处补充阶段2新增的语义)
const (
	// MemoryRecordSummary 压缩摘要记录:取代一段旧记录,自身占用一个更大的新序号。
	MemoryRecordSummary = "summary"
	// MemoryRecordInterrupted assistant 记录的中断标记(记录头形如 "## [7] assistant:interrupted | ...")
	MemoryRecordInterrupted = "interrupted"
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
func (r MemoryRecord) IsSummary() bool { return r.RecordType == MemoryRecordSummary }

// IsInterrupted 是否为被中断的助手回复(内容为中断时已产出的部分)。
func (r MemoryRecord) IsInterrupted() bool {
	return r.RecordType == HistoryRecordAssistant && r.Title == MemoryRecordInterrupted
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

// CompactionStat 供日志使用:把记录数/字符数汇总为一行。
func CompactionStat(targets []MemoryRecord) string {
	chars := 0
	for _, rec := range targets {
		chars += utf8.RuneCountInString(rec.Content)
	}
	return fmt.Sprintf("records=%d chars=%d", len(targets), chars)
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
