/*
* abstract interface for the terminal
 */

package core

type Terminator interface {
	Start(onInput func(string), onResize func())
	Stop()

	Write(s string)
	Columns() int
	Rows() int
	HideCursor()
	ShowCursor()
}
