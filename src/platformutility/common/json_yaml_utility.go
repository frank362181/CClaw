/**
* json、yaml等字符串格式工具集合
*
**/

package common

import "fmt"

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
