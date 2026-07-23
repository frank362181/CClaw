/*
* 键盘事件
 */

package core

import "strings"

type KeyCombination struct {
	Key   string
	Ctrl  bool
	Alt   bool
	Shift bool
}

// ParseKeyCombination 解析组合键
func ParseKeyCombination(data string) *KeyCombination {
	if len(data) == 0 {
		return nil
	}

	kc := &KeyCombination{}

	// 单字节控制字符
	if len(data) == 1 && data[0] < 32 {
		kc.Ctrl = true
		kc.Key = string(rune(data[0]) + 96) //转换为字母
		return kc
	}

	// CSI u序列
	if strings.HasPrefix(data, KeyReleasePrefix) && strings.HasSuffix(data, "u") {

	}

	return kc
}

/*
* checks if the input data represents a Kitty key release event.
* Simplified: real implementation would inspect the escape sequence
 */
func isKeyRelease(data string) bool {
	return strings.HasPrefix(data, KeyReleasePrefix) && strings.Count(data, ":") >= 2
}

/*
* checks for a specific key combination(e.g., "shift+ctrl+d")
* this is a stub; a real implementation would parse CSI u sequences.
 */
func matchesKey(data, key string) bool {
	switch key {
	case "shift+ctrl+d":
		return data == "\x1b[68;6u" || data == "\x1b[68;4u"
	case "ctrl+c":
		return data == "\x03"
	case "escape":
		return data == "\x1b"
	case "enter":
		return data == "\r" || data == "\n"
	case "tab":
		return data == "\t"
	}

	return false
}
