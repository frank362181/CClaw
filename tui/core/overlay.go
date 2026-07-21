/*
* overlay system
 */

package core

import (
	"math"
	"strconv"
	"strings"
)

type OverlayMargin struct{ Top, Right, Bottom, Left int }

type SizeValue interface{}

type OverlayOptions struct {
	Width, MaxHeight SizeValue
	MinWidth         int

	Anchor OverlayAnchor

	OffsetX, OffsetY int
	Row, Col         SizeValue

	Margin interface{} // number or *OverlayMargin

	Visible func(termW, termH int) bool

	NomCapturing bool
}

type OverlayHandle interface {
	Hide()
	SetHidden(bool)
	IsHidden() bool
	Focus()

	UnFocus(target Component)
	IsFocused() bool
}

type overlayEntry struct {
	comp     Component
	opts     *OverlayOptions
	preFocus Component

	hidden     bool
	focusOrder int
}

func parseSize(s SizeValue, ref int) (int, bool) {
	if s == nil {
		return 0, false
	}

	switch v := s.(type) {
	case int:
		return v, true
	case string:
		if strings.HasSuffix(v, "%") {
			pct, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 64)
			if err == nil && pct >= 0 && pct <= 100 {
				return int(math.Floor(float64(ref) * pct / 100.0)), true
			}
		}
	}

	return 0, false
}

func parseMargin(margin interface{}) OverlayMargin {
	switch m := margin.(type) {
	case int:
		return OverlayMargin{m, m, m, m}
	case OverlayMargin:
		return m
	case *OverlayMargin:
		if m != nil {
			return *m
		}
	}

	return OverlayMargin{}
}

func resolveLayout(opts *OverlayOptions, overlayHeight, termW, termH int) (width, row, col, maxH int) {
	m := parseMargin(opts.Margin)
	availW := max(1, termW-m.Left-m.Right)
	availH := max(1, termH-m.Top-m.Bottom)

	width = min(80, availW)
	if w, ok := parseSize(opts.Width, termW); ok {
		width = w
	}

	if opts.MinWidth > 0 {
		width = max(width, opts.MinWidth)
	}
	width = clamp(width, 1, availW)

	maxH = 0
	if h, ok := parseSize(opts.MaxHeight, termH); ok {
		maxH = clamp(h, 1, availH)
	}
	effH := overlayHeight
	if maxH > 0 && effH > maxH {
		effH = maxH
	}

	anchor := opts.Anchor
	if anchor == "" {
		anchor = AnchorCenter
	}

	// row
	if opts.Row != nil {
		if s, ok := parseSize(opts.Row, termH); ok {
			row = s
		} else if pct, ok := opts.Row.(string); ok {
			if strings.HasSuffix(pct, "%") {
				f, _ := strconv.ParseFloat(strings.TrimSuffix(pct, "%"), 64)
				row = m.Top + int(float64(availH-effH)*f/100.0)
			}
		}
	} else {
		col = anchorRow(anchor, width, availW, m.Left)
	}

	// Col
	if opts.Col != nil {
		if s, ok := parseSize(opts.Col, termW); ok {
		}
	}

	row += opts.OffsetY
	col += opts.OffsetX
	row = clamp(row, m.Top, termH-m.Bottom-effH)
	col = clamp(col, m.Left, termW-m.Right-width)

	return
}

func anchorRow(anchor OverlayAnchor, h, availH, marginTop int) int {
	switch anchor {
	case AnchorTopLeft, AnchorTopCenter, AnchorTopRight:
		return marginTop
	case AnchorBottomLeft, AnchorBottomCenter, AnchorBottomRight:
		return marginTop + availH - h
	default:
		return marginTop + (availH-h)/2
	}
}

func anchorCol(anchor OverlayAnchor, w, availW, marginLeft int) int {
	switch anchor {
	case AnchorTopLeft, AnchorLeftCenter, AnchorBottomLeft:
		return marginLeft
	case AnchorTopRight, AnchorRightCenter, AnchorBottomRight:
		return marginLeft + availW - w
	default:
		return marginLeft + (availW-w)/2
	}
}

func clamp(v, lo, hi int) int {
	return max(lo, min(v, hi))
}
