/*
* calculate the width, slice, and ANSI reset
 */

package core

import (
	"regexp"
	"strings"

	"github.com/mattn/go-runewidth"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
var oscRegex = regexp.MustCompile(`\x1b\].*?(\x07|\x1b\\)`)

// segment result used in compositeLineAt
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
			if strict && w < maxWidth {
				// fill remaining with spaces?
			}

			break
		}

		result.WriteRune(r)
		w += rw
	}

	return result.String()
}

// is a placeholder for any final sanitization
func normalizeTerminatorOutput(s string) string {
	return s
}

const segmentReset = "\x1b[0m\x1b]8;;\x07"

func applyLineResets(lines []string) []string {
	for i, j := range lines {
		if !isImageLine(j) {
			lines[i] = normalizeTerminatorOutput(j) + segmentReset
		}
	}

	return lines
}
