package engine

import (
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/bubbles/viewport"
)

type LogEntry struct {
	Level     string // the LEVEL of log
	Content   string
	Timestamp time.Time
}

type LogManager struct {
	program *tea.Program
	logCh   chan LogEntry
	pending []LogEntry
	mu      sync.Mutex

	flushInterval time.Duration

	quit    chan struct{} //主动停止信号
	stopped chan struct{} //用户等待协程退出
}

type ModelUI struct {
	viewport viewport.Model
	texting  textarea.Model

	logs   []string //所有日志行
	width  int
	height int
	ready  bool
	logMgr *LogManager

	currentTime time.Time // 缓存的时间，每秒更新
}

// flushMsg 是LogManager向UI发送的刷新事件
type flushMsg struct {
	Entries []LogEntry
}
