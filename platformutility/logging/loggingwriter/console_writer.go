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

import "io"

type ConsoleWriter struct {
	io.Writer
}

func (cw *ConsoleWriter) Sync() error {
	return nil
}
