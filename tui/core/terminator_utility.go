/*
* calculate the width, slice, and ANSI reset
 */

package core

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

var (
	ansiRegex = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	oscRegex  = regexp.MustCompile(`\x1b\].*?(?:\x07|\x1b\\)`)
)

// segment result used in compositeLineAt,文本分段结果
type segment struct {
	before      string
	beforeWidth int

	text  string
	width int

	after      string
	afterWidth int
}

// calculate the width of the string
func visibleWidth(s string) int {
	clean := ansiRegex.ReplaceAllString(s, "")
	clean = oscRegex.ReplaceAllString(clean, "")

	return runewidth.StringWidth(clean)
}

// 移除ANSI 转义序列
func stripANSI(s string) string {
	s = ansiRegex.ReplaceAllString(s, "")
	s = oscRegex.ReplaceAllString(s, "")
	return s
}

func sliceWithWidth(s string, start, maxWidth int, strict bool) (string, int) {
	text := sliceByColumn(s, start, maxWidth, strict)
	width := visibleWidth(text)
	return text, width
}

// 提取行片段用于覆盖层合成
func extractSegments(s string, startCol, afterStart, afterLen int, strict bool) segment {
	before := sliceByColumn(s, 0, startCol, true)
	beforeWidth := visibleWidth(before)

	rest := s[len(before):]
	overlay := sliceByColumn(rest, 0, afterStart-startCol, true)
	overlayWidth := visibleWidth(overlay)

	remaining := rest[len(overlay):]
	after := sliceByColumn(remaining, 0, afterLen, strict)
	afterWidth := visibleWidth(after)

	return segment{before, beforeWidth, overlay, overlayWidth, after, afterWidth}
}

func sliceByColumn(s string, start, maxWidth int, strict bool) string {
	if maxWidth <= 0 {
		return ""
	}

	var result strings.Builder
	w := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > maxWidth {
			if strict {
				break
			}
			// 如果不允许严格模式，继续知道下一个字符开始
			if w >= maxWidth {
				break
			}
		}

		result.WriteRune(r)
		w += rw
	}

	return result.String()
}

// is a placeholder for any final sanitization
func normalizeTerminatorOutput(s string) string {
	// 确保字符串以合法的utf-8编码
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	return s
}

func applyLineResets(lines []string) []string {
	for i, j := range lines {
		if !isImageLine(j) {
			lines[i] = normalizeTerminatorOutput(j) + SegmentReset
		}
	}

	return lines
}

func compositeLineAt(baseLine, overlayLine string, startCol, overlayWidth, totalWidth int) string {
	if isImageLine(baseLine) {
		return baseLine
	}

	afterStart := startCol + overlayWidth
	seg := extractSegments(baseLine, startCol, afterStart, totalWidth-afterStart, true)

	// 提取覆盖层文本
	overlayText, width := sliceWithWidth(overlayLine, 0, overlayWidth, true)

	// 计算填充
	beforePad := max(0, startCol-seg.beforeWidth)
	overlayPad := max(0, overlayWidth-width)
	actualBeforeWidth := max(startCol, seg.beforeWidth)
	actualOverlayWidth := max(overlayWidth, width)
	afterTarget := max(0, totalWidth-actualBeforeWidth-actualOverlayWidth)
	afterPad := max(0, afterTarget-seg.afterWidth)

	// 组合结果
	var result strings.Builder
	result.WriteString(seg.before)
	result.WriteString(strings.Repeat(" ", beforePad))
	result.WriteString(SegmentReset)
	result.WriteString(overlayText)
	result.WriteString(strings.Repeat(" ", overlayPad))
	result.WriteString(SegmentReset)
	result.WriteString(seg.after)
	result.WriteString(strings.Repeat(" ", afterPad))

	// 安全检查
	resultStr := result.String()
	resultWidth := visibleWidth(resultStr)
	if resultWidth <= totalWidth {
		return resultStr
	}

	// 截断到终端的宽度
	return sliceByColumn(resultStr, 0, totalWidth, true)
}
