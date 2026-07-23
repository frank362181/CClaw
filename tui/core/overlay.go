/*
* overlay system
 */

package core

import (
	"math"
)

// OverlayMargin 覆盖层的边框
type OverlayMargin struct{ Top, Right, Bottom, Left int }

// SizeValue 尺寸值
type SizeValue struct {
	Value     int
	IsPercent bool
}

// OverlayOptions 覆盖层的配置选项
type OverlayOptions struct {
	Width     *SizeValue
	MaxHeight *SizeValue
	MinWidth  int

	Anchor OverlayAnchor

	OffsetX int
	OffsetY int
	Row     *SizeValue
	Col     *SizeValue

	Margin *OverlayMargin // number or *OverlayMargin

	Visible func(termW, termH int) bool

	NonCapturing bool
}

// OverlayHandle 覆盖层的控制句柄
type OverlayHandle interface {
	// Hide 永久移除覆盖层
	Hide()

	// SetHidden 临时隐藏
	SetHidden(bool)
	IsHidden() bool
	Focus()

	UnFocus(target Component)
	IsFocused() bool
}

// 覆盖层堆栈条目
type overlayEntry struct {
	comp     Component
	opts     *OverlayOptions
	preFocus Component

	hidden     bool
	focusOrder int
}

func (sv *SizeValue) ToAbsolute(ref int) int {
	if sv == nil {
		return 0
	}

	if sv.IsPercent {
		return int(math.Floor(float64(ref) * float64(sv.Value) / 100.0))
	}

	return sv.Value
}

// 解析覆盖层布局
func resolveLayout(opts *OverlayOptions, overlayHeight, termW, termH int) (width, row, col, maxH int) {
	m := &OverlayMargin{}
	if opts.Margin != nil {
		*m = *opts.Margin
	}
	availW := max(1, termW-m.Left-m.Right)
	availH := max(1, termH-m.Top-m.Bottom)

	// parse width
	width = min(80, availW)
	if opts.Width != nil {
		width = opts.Width.ToAbsolute(termW)
	}

	if opts.MinWidth > 0 {
		width = max(width, opts.MinWidth)
	}
	width = max(1, min(width, availW))

	// parse height
	maxH = 0
	if opts.MaxHeight != nil {
		maxH = opts.MaxHeight.ToAbsolute(termH)
		maxH = max(1, min(maxH, availH))
	}

	// 有效高度
	effH := overlayHeight
	if maxH > 0 && effH > maxH {
		effH = maxH
	}

	// 解析行位置
	if opts.Row != nil {
		row = opts.Row.ToAbsolute(termH)
	} else {
		anchor := opts.Anchor
		if anchor == "" {
			anchor = AnchorCenter
		}
		row = resolveAnchorRow(anchor, effH, availH, m.Top)
	}

	// Col
	if opts.Col != nil {
		col = opts.Col.ToAbsolute(termW)
	} else {
		anchor := opts.Anchor
		if anchor == "" {
			anchor = AnchorCenter
		}
		col = resolveAnchorCol(anchor, width, availW, m.Left)
	}

	row += opts.OffsetY
	col += opts.OffsetX
	row = clamp(row, m.Top, termH-m.Bottom-effH)
	col = clamp(col, m.Left, termW-m.Right-width)

	return
}

func resolveAnchorRow(anchor OverlayAnchor, h, availH, marginTop int) int {
	switch anchor {
	case AnchorTopLeft, AnchorTopCenter, AnchorTopRight:
		return marginTop
	case AnchorBottomLeft, AnchorBottomCenter, AnchorBottomRight:
		return marginTop + availH - h
	default:
		return marginTop + (availH-h)/2
	}
}

func resolveAnchorCol(anchor OverlayAnchor, w, availW, marginLeft int) int {
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
