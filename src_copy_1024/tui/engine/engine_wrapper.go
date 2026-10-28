package tui_engine

import (
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"my.favorite/platformutility/common"
)

var logManager = common.GetSingleton(func() *LogManager {
	m := initializeUI(nil)
	p := tea.NewProgram(m, tea.WithAltScreen())

	lm := &LogManager{
		program:       p,
		logCh:         make(chan LogEntry, 256),
		pending:       make([]LogEntry, 0, 128),
		flushInterval: 100 * time.Millisecond, //刷新日志的时间间隔为 100ms

		quit:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	m.logMgr = lm

	return lm
}).Instance()

func GetLogManager() *LogManager {
	return logManager
}

// =================================== private function & methods =====================================
func initializeUI(logMgr *LogManager) *ModelUI {
	ta := textarea.New()
	ta.Placeholder = "输入命令或消息,Enter 发送...:"
	ta.Focus()
	ta.CharLimit = 500
	ta.ShowLineNumbers = false
	ta.MaxHeight = 3
	ta.KeyMap.InsertNewline.SetEnabled(true)
	ta.FocusedStyle.Base = textAreaStyle
	ta.BlurredStyle.Base = textAreaStyle

	vp := viewport.New(80, 30)
	vp.Style = viewportBoxStyle

	return &ModelUI{
		texting:     ta,
		viewport:    vp,
		logs:        []string{},
		logMgr:      logMgr,
		currentTime: time.Now(),
	}
}
