package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tickMsg time.Time

func (mui *ModelUI) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		tick(),
	)
}

func (mui *ModelUI) View() string {
	if !mui.ready {
		return "初始化中..."
	}

	// 标题栏
	title := titleStyle.Copy().Width(mui.width).Render("📋 LOG VIEWER (事件驱动 + 差分渲染)")

	// 状态栏：左侧日志技术，右侧当前时间
	logCount := fmt.Sprintf("日志条数：%d", len(mui.logs))
	timeStr := time.Now().Format("2006-01-02 15:04:05")
	statusLeft := statusStyle.Render(logCount)
	statusRight := statusStyle.Copy().
		Width(mui.width - lipgloss.Width(logCount) - 4).
		Align(lipgloss.Right).
		Render(timeStr)
	status := lipgloss.JoinHorizontal(lipgloss.Top, statusLeft, statusRight)

	viewportContent := viewportBoxStyle.Copy().Width(mui.width).Render(mui.viewport.View())

	// 输入区(viewport 加边框)
	inputContext := inputBoxStyle.Copy().Width(mui.width).Render(mui.texting.View())

	return lipgloss.JoinVertical(
		lipgloss.Top,
		title,
		status,
		viewportContent,
		inputContext)
}

func (mui *ModelUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
	)

	switch msg := msg.(type) {
	case tickMsg:
		mui.currentTime = time.Time(msg)
		return mui, tick()
	case tea.WindowSizeMsg:
		mui.width = msg.Width
		mui.height = msg.Height

		mui.texting.SetWidth(msg.Width - 8)

		//测量输入去的实际高度
		inputRendered := inputBoxStyle.Width(msg.Width).Render(mui.texting.View())
		inputHeight := lipgloss.Height(inputRendered)

		// 标题、状态蓝 输入区高度
		headerHeight := 1 + 1 // title + status

		// viewport 总高度
		vpTotalHeight := msg.Height - headerHeight - inputHeight
		if vpTotalHeight < 1 {
			vpTotalHeight = 1
		}

		//content height
		vpContentWidth := msg.Width - 4
		vpContentHeight := vpTotalHeight - 4
		if vpContentHeight < 1 {
			vpContentHeight = 1
		}

		mui.viewport = viewport.New(vpContentWidth, vpContentHeight)
		mui.viewport.Style = viewportBoxStyle.Copy().Width(vpContentWidth).Height(vpContentHeight)

		// 首次就绪或窗口尺寸变化时，全量重建 viewport内容
		if len(mui.logs) > 0 {
			mui.viewport.SetContent(strings.Join(mui.logs, "\n"))
			mui.viewport.GotoBottom()
		}

		mui.ready = true
	case flushMsg:
		//增量追加新的日志
		for _, entry := range msg.Entries {
			line := formatLogLine(entry)
			mui.logs = append(mui.logs, line)
		}

		// 内存控制
		if len(mui.logs) > MaxLogLines {
			start := len(mui.logs) - TrimLogLines
			mui.logs = mui.logs[start:]
		}

		if mui.ready {
			mui.viewport.SetContent(strings.Join(mui.logs, "\n"))
			mui.viewport.GotoBottom()
		}

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return mui, tea.Quit
		case tea.KeyCtrlL:
			mui.logs = []string{}
			if mui.ready {
				mui.viewport.SetContent("")
			}
		case tea.KeyEnter:
			// 用户按下Enter时发送消息
			input := strings.TrimSpace(mui.texting.Value())
			if input != "" {
				mui.logMgr.Log("INFO", "USER> "+input)
				mui.texting.Reset()
			}
		}
	}

	mui.texting, tiCmd = mui.texting.Update(msg)
	mui.viewport, vpCmd = mui.viewport.Update(msg)

	return mui, tea.Batch(tiCmd, vpCmd)
}
