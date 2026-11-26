/**
* json、yaml等字符串格式工具集合
*
**/

package common

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// =========================== JSON ===============================

func MarshalJson(value any) (string, error) {
	bytes, err := json.Marshal(value)
	if err != nil {
		return BlankString, err
	}

	return string(bytes), nil
}

func UnmarshalJson(value string, v any) error {
	if v == nil {
		return fmt.Errorf("invalid parameter")
	}

	return json.Unmarshal([]byte(value), &v)
}

// FlattenJSON 把嵌套结构压成扁平结构;超长的直接丢弃
func FlattenJSON(prefix string, value interface{}, maxLength int, out map[string]string) {
	switch v := value.(type) {
	case map[string]interface{}:
		for key, child := range v {
			FlattenJSON(JoinStr(prefix, key, "."), child, maxLength, out)
		}
	case []interface{}:
		// 标量数组压成逗号串(如files_need_ocr);对象数组只保留计数
		texts := make([]string, 0, len(v))
		for _, child := range v {
			if text, ok := ValueToString(child); ok {
				texts = append(texts, text)
			}
		}

		if len(texts) == len(v) {
			out[prefix] = strings.Join(texts, ",")
			out[prefix+".count"] = fmt.Sprintf("%d", len(v))
			return
		}

		out[prefix+".count"] = fmt.Sprintf("%d", len(v))
	case nil:
	// 空值不落字段:任务执行后的检查器就当"没有",不要塞空串
	default:
		if text, ok := ValueToString(v); ok && len(text) <= maxLength {
			out[prefix] = text
		}
	}
}

// ExtractJsonObjectFromString 从纯文本中"最后一个完整的JSON对象"
func ExtractJsonObjectFromString(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == BlankString {
		return BlankString, false
	}

	begin := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if begin < 0 || end <= begin {
		return BlankString, false
	}

	candidate := trimmed[begin : end+1]
	var probe map[string]any
	if err := UnmarshalJson(candidate, &probe); err != nil {
		return BlankString, false
	}

	return candidate, true
}

/// ========================== YAML ===============================

// YamlToJsonMap 把 yaml 解析出的参数结构递归转换为可被 json.Marshal 的 map[string]any。
// (yaml.v2 解析嵌套 map 为 map[interface{}]interface{},直接 json.Marshal 会失败。)
func YamlToJsonMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return in
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = NormalizeYamlValue(v)
	}
	return out
}

func NormalizeYamlValue(v any) any {
	switch t := v.(type) {
	case map[interface{}]interface{}:
		m := make(map[string]any, len(t))
		for k, val := range t {
			m[fmt.Sprint(k)] = NormalizeYamlValue(val)
		}
		return m
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, val := range t {
			m[k] = NormalizeYamlValue(val)
		}
		return m
	case []interface{}:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = NormalizeYamlValue(e)
		}
		return out
	default:
		return v
	}
}

func YAML2String(value interface{}) (string, bool) {
	switch t := value.(type) {
	case nil:
		return "", true // 空值合法
	case string:
		return t, true
	case bool:
		return strconv.FormatBool(t), true
	case int:
		return strconv.Itoa(t), true
	case int64:
		return strconv.FormatInt(t, 10), true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	default:
		return "", false
	}
}

// =========================== private functions for module ============================
func deepCopyJsonValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return deepCopyJsonValue(val)
	case []any:
		newSlice := make([]any, len(val))
		for i, item := range val {
			newSlice[i] = deepCopyJsonValue(item)
		}
		return newSlice
	default:
		return v
	}
}
