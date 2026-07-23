/*
*
*  TUI is the main terminator UI framework with different rendering.
 */

package core

import (
	"context"
	"time"

	"my.favorite/platformutility/common"
)

func CreateTUI(term Terminator) *TUI {
	ctx, cancel := context.WithCancel(context.Background())
	t := &TUI{
		Container:  NewContainer(),
		terminator: term,

		minInterval:  time.Millisecond * 16,
		showHWCursor: common.GetEnvString("HARDWARE_CURSOR") == "1",
		clearShrink:  common.GetEnvString("CLEAR_ON_SHRINK") != "0",

		inputListeners: make(map[int]InputListener),
		focusRestore:   overlayFocusRestore{status: restoreInactive},
		prevImageIDs:   make(map[int]struct{}),

		colorSchemaListeners: make(map[int]func(schema TerminatorColorSchema)),

		ctx:    ctx,
		cancel: cancel,
	}
	return t
}
