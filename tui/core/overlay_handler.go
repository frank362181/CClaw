package core

import "sync"

type overlayHandler struct {
	tui   *TUI
	entry *overlayEntry

	mu sync.RWMutex
}

func (h *overlayHandler) Hide() {
	h.mu.Lock()
	defer h.mu.Unlock()

	// 从堆栈中移除
	for i, e := range h.tui.overlayStack {
		if e == h.entry {
			h.tui.overlayStack = append(h.tui.overlayStack[:i], h.tui.overlayStack[i+1:]...)
			break
		}
	}

	// 恢复焦点
	if h.tui.focused == h.entry.comp {
		//topVisible := h.tui.getTop
	}
}
func (h *overlayHandler) SetHidden(bool)    {}
func (h *overlayHandler) IsHidden() bool    { return h.entry.hidden }
func (h *overlayHandler) Focus()            {}
func (h *overlayHandler) UnFocus(Component) {}
func (h *overlayHandler) IsFocused() bool   { return false }
