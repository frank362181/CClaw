package core

import (
	"context"
	"fmt"
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
	*Container

	terminator Terminator
	mu         sync.RWMutex

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

// FullRedraws 返回全量重绘次数
func (t *TUI) FullRedraws() int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.fullRedraws
}

// SetShowHardwareCursor 设置是否显示硬件光标
func (t *TUI) SetShowHardwareCursor(enabled bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.showHWCursor == enabled {
		return
	}

	t.showHWCursor = enabled
	if !enabled {
		t.terminator.HideCursor()
	}

	t.requestRender(false)
}

// SetClearOnShrink 设置内容缩小时是否清除空行
func (t *TUI) SetClearOnShrink(enabled bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.clearShrink = enabled
}

func (t *TUI) SetOnDebug(f func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onDebug = f
}

// AddInputListener 添加输入监听器
func (t *TUI) AddInputListener(listener InputListener) func() {
	t.mu.Lock()
	defer t.mu.Unlock()

	id := t.nextListenerID
	t.nextListenerID++
	t.inputListeners[id] = listener

	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		delete(t.inputListeners, id)
	}
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
func (t *TUI) Start() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.stopped = false
	t.terminator.Start(t.ctx, t.handleInput, t.handleResize)
	t.terminator.HideCursor()

	if t.colorSchemaEnabled {
		err := t.terminator.Write("\x1b[?2031h")
		if err != nil {
			return err
		}
	}
	t.queryCellSize()
	t.requestRender(false)

	return nil
}

func (t *TUI) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.stopped = true
	t.cancel()

	if t.renderTimer != nil {
		t.renderTimer.Stop()
		t.renderTimer = nil
	}

	// 禁用颜色通知方案
	if t.colorSchemaEnabled {
		err := t.terminator.Write("\x1b[?2031l")
		if err != nil {
			return err
		}
	}

	// 移动光标到末尾
	if len(t.prevLines) > 0 {
		targetRow := len(t.prevLines)
		diff := targetRow - t.hwCursorRow
		if diff > 0 {
			_ = t.terminator.Write(strings.Repeat("\x1b[B", diff))
		} else if diff < 0 {
			_ = t.terminator.Write(strings.Repeat("\x1b[A", -diff))
		}
		_ = t.terminator.Write("\r\n")
	}
	t.terminator.ShowCursor()
	return t.terminator.Stop()
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
	if t.pendingOsc11Replies > 0 && isOsc11BackgroundResponse(data) {
		if color, ok := parseOsc11BackgroundResponse(data); ok {
			t.pendingOsc11Replies--

			// notify oldest pending query
			if len(t.pendingOsc11Queries) > 0 {
				query := t.pendingOsc11Queries[0]
				t.pendingOsc11Queries = t.pendingOsc11Queries[1:]
				if !query.settled {
					query.settled = true
					if query.timer != nil {
						query.timer.Stop()
					}

					select {
					case query.resolve <- &color:
					default:
					}
				}
			}
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

	// 运行输入监听器管道
	currentData := data
	for _, listener := range t.inputListeners {
		result := listener(currentData)
		if result != nil {
			if result.Consume {
				return
			}

			if result.Data != "" {
				currentData = result.Data
			} else {
				return
			}
		}
	}

	// cell size response
	if ok, h, w := consumeCellSizeResponse(data); ok {
		SetCellDimensions(w, h)
		t.Invalidate()
		t.requestRender(false)
		return
	}

	// 调试快捷键
	if matchesKey(data, "shift+ctrl+d") && t.onDebug != nil {
		t.onDebug()
		return
	}

	// focus overlay handling
	if focusedOverlay := t.findOverlayByComponent(t.focused); focusedOverlay != nil && !t.isOverlayVisible(focusedOverlay) {
		topVisible := t.getTopmostVisibleOverlay()
		if topVisible != nil {
			t.setFocus(topVisible.comp, true)
		} else {
			t.setFocus(topVisible.comp, false)
		}
	}

	// 转发焦点组件
	if t.focused != nil {
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

	// 处理覆盖层焦点恢复逻辑
	prevFocusOverlay := t.findOverlayByComponent(prev)
	if prevFocusOverlay != nil && t.isComponentInOverlayChain(prevFocusOverlay, c) {
		// 前一个焦点不在覆盖层上，且新焦点不再该覆盖层的祖先链上
		t.focusRestore = overlayFocusRestore{
			status:    restoreEligible,
			overlay:   prevFocusOverlay,
			blockedBy: nil,
		}
	}

	// apply overlay focus
	t.focused = c
	if c != nil {
		if f, ok := c.(Focusable); ok {
			f.SetFocused(true)
		}
	}

	// 恢复覆盖层焦点状态
	if entry := t.findOverlayByComponent(c); entry != nil && t.isOverlayVisible(entry) {
		t.focusRestore = overlayFocusRestore{
			status: restoreInactive,
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
			t.renderTimer = nil
		}
		t.renderRequested = true

		// 异步执行 渲染
		go func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			if t.stopped || !t.renderRequested {
				return
			}

			t.renderRequested = false
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

// 执行渲染
func (t *TUI) doRender() {
	w, h := t.terminator.Columns(), t.terminator.Rows()
	newLines := t.Container.Render(w) //渲染所有的组件

	// composite overlays
	if len(t.overlayStack) > 0 {
		newLines = t.compositeOverlays(newLines, w, h)
	}

	// extract the cursor
	cursorPos := t.extractCursorPosition(newLines, h)
	newLines = applyLineResets(newLines)

	// full render conditions
	if t.prevLines == nil || w != t.prevW || (h != t.prevH && !t.isTermux()) ||
		(t.clearShrink && len(newLines) < t.maxLinesRendered && len(t.overlayStack) == 0) {
		t.fullRender(newLines, true)
	} else {
		t.diffRender(newLines, w, h)
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
	b.WriteString("\x1b[?2026h") //开始同步更新
	if clear {
		b.WriteString(t.deleteImageIDs(t.prevImageIDs))
		b.WriteString("\x1b[2J\x1b[H\x1b[3J") //清除屏幕和回滚缓冲区
	}

	for i, l := range newLines {
		if i > 0 {
			b.WriteString("\r\n")
		}

		//处理多行图像
		if isImageLine(l) {
			reservedRows := getImageReservedRows(newLines, i, len(l)-1)
			if reservedRows > 1 {
				for row := 1; row < reservedRows; row++ {
					b.WriteString("\r\n")
				}

				b.WriteString(fmt.Sprintf("\x1b[%dA", reservedRows-1))
				b.WriteString(l)
				b.WriteString(fmt.Sprintf("\x1b[%dB", reservedRows-1))
				i += reservedRows - 1
				continue
			}
		}

		b.WriteString(l)
	}

	b.WriteString("\x1b[?2026l") //结束同步更新
	_ = t.terminator.Write(b.String())
	t.cursorRow = max(0, len(newLines)-1)
	t.hwCursorRow = t.cursorRow
	t.maxLinesRendered = len(newLines)
	t.prevViewTop = max(0, len(newLines)-t.terminator.Rows())
}

func (t *TUI) diffRender(newLines []string, w, h int) {
	firstChanged, lastChanged := -1, -1 //查找变化范围

	for i := 0; i < max(len(newLines), len(t.prevLines)); i++ {
		var old, nu string
		if i < len(t.prevLines) {
			old = t.prevLines[i]
		}
		if i < len(newLines) {
			nu = newLines[i]
		}
		if old != nu {
			if firstChanged == -1 {
				firstChanged = i
			}
			lastChanged = i
		}
	}

	//处理追加的行
	if len(newLines) > len(t.prevLines) {
		if firstChanged == -1 {
			firstChanged = len(t.prevLines)
		}

		lastChanged = len(newLines) - 1
	}

	// 无变化
	if firstChanged == -1 {
		return
	}

	// 扩展范围包含图像
	firstChanged, lastChanged = t.expandForImages(firstChanged, lastChanged, newLines)

	//检查是否需要全量渲染
	if firstChanged < t.prevViewTop {
		t.fullRender(newLines, true)
		return
	}

	// 执行差分渲染
	var b strings.Builder
	b.WriteString("\x1b[?2026h")
	b.WriteString(t.deleteChangedImages(firstChanged, lastChanged))

	// 移动光标的第一个变化行
	moveTarget := firstChanged
	if len(newLines) > len(t.prevLines) && firstChanged == len(t.prevLines) && firstChanged > 0 {
		moveTarget = lastChanged - 1
	}
	diff := moveTarget - t.hwCursorRow
	if diff > 0 {
		b.WriteString(strings.Repeat("\x1b[B", diff))
	} else if diff < 0 {
		b.WriteString(strings.Repeat("\x1b[A", -diff))
	}

	b.WriteString("\r")
	if len(newLines) > len(t.prevLines) && firstChanged == len(t.prevLines) {
		b.WriteString("\n")
	}

	// 渲染变化的行
	renderEnd := min(lastChanged, len(newLines)-1)
	for i := firstChanged; i <= renderEnd; i++ {
		if i > firstChanged {
			b.WriteString("\r\n")
		}
		b.WriteString("\x1b[2K") //清除当前行
		if !isImageLine(newLines[i]) && visibleWidth(newLines[i]) > w {
			t.handleRenderError(newLines, i, w)
			return
		}

		b.WriteString(newLines[i])
	}
	t.hwCursorRow = renderEnd

	//清除多余的行
	if len(t.prevLines) > len(newLines) {
		extraLines := len(t.prevLines) - len(newLines)
		for i := 0; i < extraLines; i++ {
			b.WriteString("\r\n\x1b[2K")
		}
		b.WriteString(fmt.Sprintf("\x1b[%dA", extraLines))
	}

	b.WriteString("\x1b[?2026l")
	_ = t.terminator.Write(b.String())
	t.cursorRow = max(0, len(newLines)-1)
	t.maxLinesRendered = max(t.maxLinesRendered, len(newLines))
	t.prevViewTop = max(0, len(newLines)-h)
}

// 合成覆盖层
func (t *TUI) compositeOverlays(base []string, w, h int) []string {
	if len(t.overlayStack) == 0 {
		return base
	}

	result := make([]string, len(base))
	copy(result, base)

	// 收集可见覆盖层并排序
	type renderedOverlay struct {
		lines []string
		row   int
		col   int
		w     int
	}

	var visibleOverlays []*overlayEntry
	for _, entry := range t.overlayStack {
		if t.isOverlayVisible(entry) {
			visibleOverlays = append(visibleOverlays, entry)
		}
	}

	// 按focusOrder排序
	for i := 0; i < len(visibleOverlays)-1; i++ {
		for j := i + 1; j < len(visibleOverlays); j++ {
			if visibleOverlays[i].focusOrder > visibleOverlays[j].focusOrder {
				visibleOverlays[i], visibleOverlays[j] = visibleOverlays[j], visibleOverlays[i]
			}
		}
	}

	var rendered []renderedOverlay
	minLinesNeeded := len(result)
	for _, entry := range visibleOverlays {
		opts := entry.opts
		if opts == nil {
			opts = &OverlayOptions{}
		}

		// 解析布局
		w, _, _, maxH := resolveLayout(opts, 0, w, h)
		overlayLines := entry.comp.Render(w)
		if maxH > 0 && len(overlayLines) > maxH {
			overlayLines = overlayLines[:maxH]
		}

		w, row, col, _ := resolveLayout(opts, len(overlayLines), w, h)
		rendered = append(rendered, renderedOverlay{overlayLines, row, col, w})
		minLinesNeeded = max(minLinesNeeded, row+len(overlayLines))
	}

	// 扩展结果数组
	workingHeight := max(len(result), max(h, minLinesNeeded))
	for len(result) < workingHeight {
		result = append(result, "")
	}

	viewportStart := max(0, workingHeight-h)

	// 合成每个覆盖层
	for _, r := range rendered {
		for i, line := range r.lines {
			idx := viewportStart + r.row + i
			if idx >= 0 && idx < len(result) {
				truncatedLine := line
				if visibleWidth(line) > r.w {
					truncatedLine = sliceByColumn(line, 0, r.w, true)
				}
				result[idx] = compositeLineAt(result[idx], truncatedLine, r.col, r.w, w)
			}
		}
	}

	return result
}

func (t *TUI) extractCursorPosition(lines []string, h int) *[2]int {
	viewportTop := max(0, len(lines)-h)

	for i := len(lines) - 1; i >= viewportTop; i-- {
		idx := strings.Index(lines[i], CursorMarker)
		if idx >= 0 {
			col := visibleWidth(lines[i][:idx])
			lines[i] = lines[i][:idx] + lines[i][idx+len(CursorMarker):]
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
	_ = t.terminator.Write("\x1b[16t")
}

func (t *TUI) findOverlayByComponent(c Component) *overlayEntry {
	for _, entry := range t.overlayStack {
		if entry.comp == c {
			return entry
		}
	}

	return nil
}

func (t *TUI) isOverlayInStack(entry *overlayEntry) bool {
	for _, overlay := range t.overlayStack {
		if overlay == entry {
			return true
		}
	}

	return false
}

func (t *TUI) isComponentInOverlayChain(entry *overlayEntry, c Component) bool {
	visited := make(map[Component]bool)
	current := entry.preFocus

	for current != nil && !visited[current] {
		visited[current] = true
		if current == c {
			return true
		}

		if overlayEntry := t.findOverlayByComponent(current); overlayEntry != nil {
			current = overlayEntry.preFocus
		} else {
			break
		}
	}

	return false
}

func (t *TUI) getTopmostVisibleOverlay() *overlayEntry {
	var topmost *overlayEntry

	for _, entry := range t.overlayStack {
		if entry.opts != nil && entry.opts.NonCapturing {
			continue
		}

		if !t.isOverlayVisible(entry) {
			continue
		}

		if topmost == nil || entry.focusOrder > topmost.focusOrder {
			topmost = entry
		}
	}

	return topmost
}

func (t *TUI) isOverlayVisible(e *overlayEntry) bool {
	if e.hidden {
		return false
	}

	if e.opts != nil && e.opts.Visible != nil {
		return e.opts.Visible(t.terminator.Columns(), t.terminator.Rows())
	}

	return true
}

func (t *TUI) expandForImages(firstChanged, lastChanged int, newLines []string) (int, int) {
	expandedFirst := firstChanged
	expandedLast := lastChanged

	// 扩展前一次渲染中的图像范围
	for i := 0; i < len(t.prevLines); i++ {
		ids := extractImageIDs(t.prevLines[i])
		if len(ids) == 0 {
			continue
		}

		blockEnd := i + getImageReservedRows(t.prevLines, i, len(t.prevLines)-1) - 1
		if i >= firstChanged || (i <= lastChanged && blockEnd >= firstChanged) {
			expandedFirst = min(expandedFirst, i)
			expandedLast = max(expandedLast, blockEnd)
		}
	}

	// 扩展新渲染中的图像范围
	for i := 0; i < len(newLines); i++ {
		ids := extractImageIDs(newLines[i])
		if len(ids) == 0 {
			continue
		}

		blockEnd := i + getImageReservedRows(newLines, i, len(newLines)-1) - 1
		if i >= firstChanged || (i <= lastChanged && blockEnd >= firstChanged) {
			expandedFirst = min(expandedFirst, i)
			expandedLast = max(expandedLast, blockEnd)
		}
	}

	return expandedFirst, expandedLast
}

func (t *TUI) handleRenderError(lines []string, index, width int) {

}
