package core

import (
	"context"
	"strings"
	"sync"
	"time"

	"my.favorite/platformutility/common"
)

// InputListenerResult 输入监听器返回结果
type InputListenerResult struct {
	Consume bool   //是否消费事件
	Data    string //修改后的数据，若为空则丢弃
}
type InputListener func(data string) *InputListenerResult

// 恢复动作
type resumeAction struct {
	kind   string // restore-overlay,focus-target
	target Component
}

// 覆盖层焦点回复状态
type overlayFocusRestore struct {
	status  string // inactive,eligible,blocked
	overlay *overlayEntry

	blockedBy Component
	resume    resumeAction
}

// 待处理的 OSC 11查询
type pendingOsc11Query struct {
	settled bool
	resolve chan<- *RGBColor
	timer   *time.Timer
}

type TUI struct {
	Container

	terminator Terminator
	mu         sync.Mutex

	//渲染状态
	prevLines    []string
	prevImageIDs map[int]struct{}
	prevW, prevH int
	focused      Component

	inputListeners map[int]InputListener
	nextListenerID int

	onDebug func()

	// 渲染调度
	renderRequested bool
	renderTimer     *time.Timer
	lastRender      time.Time
	minInterval     time.Duration

	// 光标状态
	cursorRow, hwCursorRow    int
	showHWCursor, clearShrink bool

	maxLinesRendered, prevViewTop int
	fullRedraws                   int
	stopped                       bool

	// 覆盖层
	overlayStack  []*overlayEntry
	focusOrderCtr int

	focusRestore overlayFocusRestore

	// OSC 11背景色查询
	pendingOsc11Replies int
	pendingOsc11Queries []pendingOsc11Query

	// 颜色方案监听
	colorSchemaListeners map[int]func(schema TerminatorColorSchema)
	colorSchemaEnabled   bool

	nextColorSchemaID int

	//上下文控制
	ctx    context.Context
	cancel context.CancelFunc
}

func (t *TUI) SetFocus(c Component) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.setFocus(c, true)
}

func (t *TUI) RequestRender(force bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.requestRender(force)
}

// Start actives the TUI
func (t *TUI) Start() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.stopped = false
	t.terminator.Start(t.handleInput, t.handleResize)
	t.terminator.HideCursor()

	if t.colorSchemaEnabled {
		t.terminator.Write("\x1b[?2031h")
	}
	t.queryCellSize()
	t.requestRender(false)
}

func (t *TUI) ShowOverlay(comp Component, opts *OverlayOptions) OverlayHandle {
	t.mu.Lock()
	defer t.mu.Unlock()

	entry := &overlayEntry{
		comp:       comp,
		opts:       opts,
		preFocus:   t.focused,
		focusOrder: t.focusOrderCtr + 1,
	}

	t.focusOrderCtr++
	t.overlayStack = append(t.overlayStack, entry)
	if !opts.NonCapturing && t.isOverlayVisible(entry) {
		t.setFocus(comp, true)
	}

	t.terminator.HideCursor()
	t.requestRender(false)
	return &overlayHandler{tui: t, entry: entry}
}

func (t *TUI) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopped = true
	t.cancel()

	if t.renderTimer != nil {
		t.renderTimer.Stop()
	}
	if t.colorSchemaEnabled {
		t.terminator.Write("\x1b[?2031l")
	}

	// 移动光标到末尾
	if len(t.prevLines) > 0 {
		targetRow := len(t.prevLines)
		diff := targetRow - t.hwCursorRow
		if diff > 0 {
			t.terminator.Write(strings.Repeat("\x1b[B", diff))
		} else if diff < 0 {
			t.terminator.Write(strings.Repeat("\x1b[A", -diff))
		}
		t.terminator.Write("\r\n")
	}
	t.terminator.ShowCursor()
	t.terminator.Stop()
}

// ========================= private functions & methods =====================
// 窗口大小变化处理
func (t *TUI) handleResize() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.requestRender(false)
}

func (t *TUI) handleInput(data string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	//OSC 11
	if t.pendingOsc11Replies > 0 && isOsc11Response(data) {
		if col, ok := parseOsc11Response(data); ok {
			t.pendingOsc11Replies--
			// notify oldest pending query
		}
		return
	}

	//Color schema report
	if schema, ok := parseColorSchemaReport(data); ok {
		for _, cb := range t.colorSchemaListeners {
			cb(schema)
		}
		return
	}

	// cell size response
	if ok, h, w := consumeCellSizeResponse(data); ok {
		setCellDimensions(w, h)
		t.Invalidate()
		t.requestRender(false)
		return
	}

	// input listeners
	for _, l := range t.inputListeners {
		res := l(data)
		if res != nil {
			if res.Consume {
				return
			}
			if res.Data != "" {
				data = res.Data
			} else {
				return
			}
		}
	}

	// focus overlay handling
	if t.focused != nil && t.focused.HandleInput != nil {
		if isKeyRelease(data) && !t.focused.WantsKeyRelease() {
			return
		}

		t.focused.HandleInput(data)
		t.requestRender(false)
	}
}

func (t *TUI) setFocus(c Component, clearRestore bool) {
	// clear previous focusable
	prev := t.focused
	if prev != nil {
		if f, ok := prev.(Focusable); ok {
			f.SetFocused(false)
		}
	}

	// apply overlay focus
	t.focused = c
	if c != nil {
		if f, ok := c.(Focusable); ok {
			f.SetFocused(true)
		}
	}
}

func (t *TUI) requestRender(force bool) {
	if force {
		t.prevLines = nil
		t.prevW = -1
		t.prevH = -1
		t.cursorRow = 0
		t.hwCursorRow = 0
		t.maxLinesRendered = 0
		t.prevViewTop = 0
		if t.renderTimer != nil {
			t.renderTimer.Stop()
		}
		go func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			if t.stopped {
				return
			}

			t.lastRender = time.Now()
			t.doRender()
		}()
		return
	}

	if t.renderRequested {
		return
	}

	t.renderRequested = true
	go t.scheduleRender()
}

func (t *TUI) doRender() {
	w, h := t.terminator.Columns(), t.terminator.Rows()
	newLines := t.Container.Render(w)

	// composite overlays
	if len(t.overlayStack) > 0 {
		newLines = t.compositeOverlays(newLines, w, h)
	}

	cursorPos := t.extractCursor(newLines, h)
	newLines = applyLineResets(newLines)

	// full render conditions
	if t.prevLines == nil || w != t.prevW || (h != t.prevH && !t.isTermux()) ||
		(t.clearShrink && len(newLines) < t.maxLinesRendered && len(t.overlayStack) == 0) {
		t.fullRender(newLines, true)
	} else {
		t.diffRender(newLines, w, h, cursorPos)
	}

	t.prevLines = newLines
	t.prevImageIDs = t.collectImageIDs(newLines)
	t.prevW = w
	t.prevH = h
	t.positionCursor(cursorPos, len(newLines))
}

func (t *TUI) scheduleRender() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stopped || !t.renderRequested {
		return
	}

	delay := t.minInterval - time.Since(t.lastRender)
	if delay < 0 {
		delay = 0
	}
	if t.renderTimer != nil {
		t.renderTimer.Stop()
	}
	t.renderTimer = time.AfterFunc(delay, func() {
		t.mu.Lock()
		defer t.mu.Unlock()

		if t.stopped || !t.renderRequested {
			return
		}

		t.renderRequested = false
		t.lastRender = time.Now()
		t.doRender()
		if t.renderRequested {
			t.scheduleRender()
		}
	})
}

func (t *TUI) fullRender(newLines []string, clear bool) {
	t.fullRedraws++
	var b strings.Builder
	b.WriteString("\x1b[?2026h")
	if clear {
		b.WriteString(t.deleteImageIDs(t.prevImageIDs))
		b.WriteString("\x1b[2J\x1b[H\x1b[3J")
	}

	for i, l := range newLines {
		if i > 0 {
			b.WriteString("\r\n")
		}
		b.WriteString(l)
	}

	b.WriteString("\x1b[?2026l")
	t.terminator.Write(b.String())
	t.cursorRow = len(newLines) - 1
	t.hwCursorRow = t.cursorRow
	t.maxLinesRendered = len(newLines)
	t.prevViewTop = max(0, len(newLines)-t.terminator.Rows())
}

func (t *TUI) diffRender(newLines []string, w, h int, cursorPos *[2]int) {
	first, last := -1, -1

	for i := 0; i < max(len(newLines), len(t.prevLines)); i++ {
		var old, nu string
		if i < len(t.prevLines) {
			old = t.prevLines[i]
		}
		if i < len(newLines) {
			nu = newLines[i]
		}
		if old != nu {
			if first == -1 {
				first = i
			}
			last = i
		}
	}

	if first == -1 {
		t.positionCursor(cursorPos, len(newLines))
		return
	}

	// move cursor and update changed lines
	var b strings.Builder
	b.WriteString("\x1b[?2026h")
	b.WriteString(t.deleteChangedImages(first, last))

	// cursor movement
	diff := first - t.hwCursorRow
	if diff > 0 {
		b.WriteString(strings.Repeat("\x1b[B", diff))
	} else if diff < 0 {
		b.WriteString(strings.Repeat("\x1b[A", -diff))
	}

	for i := first; i <= last && i < len(newLines); i++ {
		b.WriteString("\r\x1b[2K")
		b.WriteString(newLines[i])
		t.hwCursorRow = i
		if i < last {
			b.WriteString("\r\n")
			t.hwCursorRow++
		}
	}
	b.WriteString("\x1b[?2026l")
	t.terminator.Write(b.String())
	t.cursorRow = len(newLines) - 1
	t.maxLinesRendered = max(t.maxLinesRendered, len(newLines))
	t.prevViewTop = max(0, len(newLines)-h)
}

// merges overlay content into the base lines
func (t *TUI) compositeOverlays(base []string, w, h int) []string {
	// sort visible overlays by focusOrder, render, and overwrite base lines
	// full implementation omitted for brevity;
	return base
}

func (t *TUI) extractCursor(lines []string, h int) *[2]int {
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-h; i-- {
		idx := strings.Index(lines[i], cursorMarker)
		if idx >= 0 {
			col := visibleWidth(lines[i][:idx])
			lines[i] = lines[i][:idx] + lines[i][idx+len(cursorMarker):]
			return &[2]int{i, col}
		}
	}

	return nil
}

func (t *TUI) positionCursor(pos *[2]int, total int) {
	if pos == nil {
		t.terminator.HideCursor()
		return
	}

	row := clamp(pos[0], 0, total-1)
	col := pos[1]

	// move cursor
	var b strings.Builder
	diff := row - t.hwCursorRow
	if diff > 0 {
		b.WriteString(strings.Repeat("\x1b[B", diff))
	} else if diff < 0 {
		b.WriteString(strings.Repeat("\x1b[A", -diff))
	}
	b.WriteString(strings.Repeat("\x1b[%dG", col+1))
	if b.Len() > 0 {
		t.terminator.Write(b.String())
	}
	t.hwCursorRow = row
	if t.showHWCursor {
		t.terminator.ShowCursor()
	} else {
		t.terminator.HideCursor()
	}
}

func (t *TUI) collectImageIDs(lines []string) map[int]struct{} {
	ids := make(map[int]struct{})
	for _, l := range lines {
		for _, id := range extractImageIDs(l) {
			ids[id] = struct{}{}
		}
	}

	return ids
}

func (t *TUI) deleteImageIDs(ids map[int]struct{}) string {
	var b strings.Builder
	for id := range ids {
		b.WriteString(deleteImage(id))
	}
	return b.String()
}

func (t *TUI) deleteChangedImages(first, last int) string {
	ids := make(map[int]struct{})
	for i := first; i <= last && i < len(t.prevLines); i++ {
		for _, id := range extractImageIDs(t.prevLines[i]) {
			ids[id] = struct{}{}
		}
	}

	return t.deleteImageIDs(ids)
}

func (t *TUI) isTermux() bool {
	return common.GetEnvString("TERMUX_VERSION") != ""
}

func (t *TUI) queryCellSize() {
	t.terminator.Write("\x1b[16t")
}

func (t *TUI) isOverlayVisible(e *overlayEntry) bool {
	if e.hidden {
		return false
	}

	if e.opts.Visible != nil {
		return e.opts.Visible(t.terminator.Columns(), t.terminator.Rows())
	}

	return true
}
