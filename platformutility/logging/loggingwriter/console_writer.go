/*
* @Summary
*	if printing the log to console in WINDOWS, os.Stdout will be invalid
*	to define this class to avoid this issues
* @Date
*	Jun 28, 2026
* @Author
*	Frank Ng
 */

package loggingwriter

import "os"

type ConsoleWriter struct {
	*os.File
}

func (cw *ConsoleWriter) Sync() error {
	return nil
}
