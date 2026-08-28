package common

import (
	"encoding/json"
	"strconv"
	"strings"
)

// IsCJK 判断字符是否属于中日韩(CJK)文字区。
// 这些字符在多数 tokenizer 中每字符约 0.5~1 token,与拉丁字符(约 4 字符/token)差异明显,
// 分开统计可让估算更贴近真实用量。
func IsCJK(r rune) bool {
	switch {
	case r >= 0x3400 && r <= 0x4DBF, // CJK 扩展 A
		r >= 0x4E00 && r <= 0x9FFF, // CJK 统一表意文字
		r >= 0x3040 && r <= 0x30FF, // 平假名/片假名
		r >= 0xAC00 && r <= 0xD7AF, // 谚文(Hangul)
		r >= 0xF900 && r <= 0xFAFF: // CJK 兼容表意文字
		return true
	default:
		return false
	}
}

// SplitString 把点分路径拆成段，并忽略空段（如 "a..b" 视为 "a.b"）。
func SplitString(dottedKey string) []string {
	raw := strings.Split(dottedKey, ".")
	segments := make([]string, 0, len(raw))
	for _, seg := range raw {
		seg = strings.TrimSpace(seg)
		if len(seg) > 0 {
			segments = append(segments, seg)
		}
	}
	return segments
}

// ValueToString 把配置树节点转换为字符串。
// 对象/数组返回其 JSON 文本；nil 视为不存在（返回 false）;其余类型转换为字符串表示。
func ValueToString(v interface{}) (string, bool) {
	switch value := v.(type) {
	case nil:
		return "", false
	case string:
		return value, true
	case bool:
		return strconv.FormatBool(value), true
	case json.Number:
		return value.String(), true
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(value), 'f', -1, 32), true
	case int:
		return strconv.Itoa(value), true
	case int64:
		return strconv.FormatInt(value, 10), true
	case uint64:
		return strconv.FormatUint(value, 10), true
	default:
		// map / slice / 其他自定义类型：序列化为 JSON 文本
		content, err := json.Marshal(value)
		if err != nil {
			return "", false
		}
		return string(content), true
	}
}

// ToJson 把字符串值转换为 JSON 兼容类型：先识别 bool，再识别数字，否则按字符串处理。
// 数字以 json.Number 形式保存，保留原字面量精度。
func ToJson(value string) interface{} {
	value = strings.TrimSpace(value)
	switch value {
	case "true":
		return true
	case "false":
		return false
	}

	num := json.Number(value)
	if _, err := num.Float64(); err == nil {
		return num
	}
	return value
}

// TruncateRunes 按 rune 安全截断,避免截断多字节字符;fast-path 直接按字节比较。
func TruncateRunes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}

	runes := []rune(s)
	if len(runes) <= max {
		return s
	}

	return string(runes[:max])
}
