package engine

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func (lm *LogManager) Start() {
	go func() {
		defer close(lm.stopped) //标记协程已经退出

		ticker := time.NewTicker(lm.flushInterval)
		defer ticker.Stop()

		for {
			select {
			case entry, ok := <-lm.logCh:
				if !ok {
					lm.flush() //通道被关闭，日志清理并退出
					return
				}
				lm.mu.Lock()
				lm.pending = append(lm.pending, entry)
				lm.mu.Unlock()
			case <-ticker.C:
				lm.flush()
			case <-lm.quit:
				lm.flush()
				return
			}
		}
	}()
}

func (lm *LogManager) Log(level, content string) {
	entry := LogEntry{
		Level:     level,
		Content:   content,
		Timestamp: time.Now(),
	}
	lm.logCh <- entry
}

func (lm *LogManager) GetP() *tea.Program {
	return lm.program
}

func (lm *LogManager) Stop() {
	close(lm.quit)
	<-lm.stopped

	//协程退出
	close(lm.logCh)
	lm.pending = nil
}

// =========================== private functions & methods ===================
func (lm *LogManager) flush() {
	lm.mu.Lock()

	if len(lm.pending) == 0 {
		lm.mu.Unlock()
		return
	}

	entries := make([]LogEntry, len(lm.pending))
	copy(entries, lm.pending)
	lm.pending = lm.pending[:0]
	lm.mu.Unlock()

	lm.program.Send(flushMsg{Entries: entries})
}
