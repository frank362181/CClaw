/*
* 键盘事件
 */

package core

import "strings"

/*
* checks if the input data represents a Kitty key release event.
* Simplified: real implementation would inspect the escape sequence
 */
func isKeyRelease(data string) bool {
	return strings.HasPrefix(data, "\x1b[") && strings.Contains(data, ":")
}

/*
* checks for a specific key combination(e.g., "shift+ctrl+d")
* this is a stub; a real implementation would parse CSI u sequences.
 */
func matchesKey(data, key string) bool {
	if key == "shift+ctrl+d" {
		return data == "\x1b[68;4u" //hypothetical
	}

	return false
}
