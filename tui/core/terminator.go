/*
* abstract interface for the terminal
 */

package core

import (
	"context"
	"io"
)

// TerminatorSize 终端尺寸大小
type TerminatorSize struct {
	Width        int
	Height       int
	PixelsWidth  int
	PixelsHeight int
}

type Terminator interface {
	Start(ctx context.Context, onInput func(string), onResize func())
	Stop() error

	// Write 写入原始数据
	Write(s string) error

	Columns() int
	Rows() int
	HideCursor()
	ShowCursor()

	// EnableMouseEvent 禁用或启用键盘事件
	EnableMouseEvent()
	DisableMouseEvent()

	// EnableBracketedPaste 启用或禁用括号粘贴模式
	EnableBracketedPaste()
	DisableBracketedPaste()

	// EnterAltScreen 进入或退出备用屏幕
	EnterAltScreen()
	ExitAltScreen()

	// RawMode 返回原始模式的文件描述符
	RawMode() io.Writer
}
