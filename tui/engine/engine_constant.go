package engine

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	MaxLogLines  = 5000 //最大的日志记录数
	TrimLogLines = 2000 //执行截断的日志记录数

	InputAreaHeight = 5 //固定输入去高度
)

// ==============================定义颜色和样式========================
var (
	primary   = lipgloss.Color("#00ADD8")
	secondary = lipgloss.Color("#FFB86C")
	danger    = lipgloss.Color("#FF5555")
	success   = lipgloss.Color("#50FA7B")
	muted     = lipgloss.Color("#6272A4")

	bgViewport = lipgloss.Color("#1E1E2E")
	bgInput    = lipgloss.Color("#16162A")
	bgTitle    = lipgloss.Color("#0D0D1A")

	textPrimary   = lipgloss.Color("#F8F8F2")
	textSecondary = lipgloss.Color("#BDC3C7")

	//标题栏样式
	titleStyle = lipgloss.NewStyle().
		Background(bgTitle).
		Foreground(primary).
		Bold(true).
		Padding(0, 1).
		Align(lipgloss.Center)

	// 状态栏样式
	statusStyle = lipgloss.NewStyle().
		Background(bgTitle).
		Foreground(muted).
		Padding(0, 1)

	// 输出区（viewport）带圆角边框
	viewportBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(primary).
		Background(bgViewport).
		Foreground(textPrimary).
		Padding(0, 1)

	textAreaStyle = lipgloss.NewStyle().
		Foreground(textPrimary).
		Background(bgInput)

	// textInput box的包装样式
	inputBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(primary).
		Background(bgInput).
		Padding(0, 1)

	levelColor = map[string]lipgloss.Style{
		"INFO":    lipgloss.NewStyle().Foreground(success).Bold(true),
		"WARNING": lipgloss.NewStyle().Foreground(secondary).Bold(true),
		"ERROR":   lipgloss.NewStyle().Foreground(danger).Bold(true),
		"DEBUG":   lipgloss.NewStyle().Foreground(muted),
		"FETAL":   lipgloss.NewStyle().Foreground(danger).Bold(true),
	}
)
