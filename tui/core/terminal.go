package core

type Terminal interface {
	Start(onInput func(string), onResize func())
	Stop()

	Write(s strig)
	Columns() int
	Rows() int
	HideCursor()
	ShowCursor()
}
