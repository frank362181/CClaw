package engine

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func formatLogLine(entry LogEntry) string {
	levelStyle, ok := levelColor[entry.Level]
	if !ok {
		levelStyle = lipgloss.NewStyle().Foreground(muted)
	}
	levelStr := levelStyle.Render(fmt.Sprintf("[%s]", entry.Level))
	timeStr := lipgloss.NewStyle().Foreground(muted).Render(entry.Timestamp.Format("15:04:05"))

	return fmt.Sprintf("%s %s %s", timeStr, levelStr, entry.Content)
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
