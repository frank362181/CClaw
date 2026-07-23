package core

const CursorMarker = "\x1b_pi:c\x07" //光标位置标记（APC序列，零宽度）

const ImagePrefix = "\x1b_G"

const KeyReleasePrefix = "\x1b[" // 键盘协议的释放事件前缀

const SegmentReset = "\x1b[0m\x1b]8;;\x07"

type OverlayAnchor string

const (
	AnchorCenter       OverlayAnchor = "center"
	AnchorTopLeft      OverlayAnchor = "top-left"
	AnchorTopRight     OverlayAnchor = "top-right"
	AnchorBottomLeft   OverlayAnchor = "bottom-left"
	AnchorBottomRight  OverlayAnchor = "bottom-right"
	AnchorTopCenter    OverlayAnchor = "top-center"
	AnchorBottomCenter OverlayAnchor = "bottom-center"
	AnchorLeftCenter   OverlayAnchor = "left-center"
	AnchorRightCenter  OverlayAnchor = "right-center"
)

// 焦点回复状态常量
const (
	restoreInactive = "interactive"
	restoreEligible = "eligible"
	restoreBlocked  = "blocked"
)

type TerminatorColorSchema string

const (
	SchemaUnknown TerminatorColorSchema = "unknown"
	SchemaDark    TerminatorColorSchema = "dark"
	SchemaLight   TerminatorColorSchema = "light"
)
