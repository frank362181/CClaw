package core

type overlayHandler struct {
	tui   *TUI
	entry *overlayEntry
}

func (h *overlayHandler) Hide()             {}
func (h *overlayHandler) SetHidden(bool)    {}
func (h *overlayHandler) IsHidden() bool    { return h.entry.hidden }
func (h *overlayHandler) Focus()            {}
func (h *overlayHandler) UnFocus(Component) {}
func (h *overlayHandler) IsFocused() bool   { return false }
