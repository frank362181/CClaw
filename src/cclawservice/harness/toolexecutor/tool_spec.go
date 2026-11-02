/**
* 定义工具在执行期间的全部元数据
*
**/

package toolexecutor

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"my.favorite/cclawservice/exception"
	pCommon "my.favorite/platformutility/common"
)

type ToolSpec struct {
	Name       string
	Script     string         // 脚本的绝对路径
	Schema     map[string]any // 参数: JSON schema(来自 SKILL.md tools[].parameters)
	ReadOnly   bool           // SKILL.md 中新增 'read-only:true'; 系统 read/glob/grep 恒 true
	Sandboxed  bool           // 需要沙箱环境(bash/write/edit);与只读互斥
	PathFields []string

	Shelled bool //命令类执行工具,可执行任意命令
}

// Normalize 清洗字段并修正互斥关系
func (s ToolSpec) Normalize() ToolSpec {
	out := s

	out.Name = strings.ToLower(strings.TrimSpace(out.Name))
	out.Script = strings.TrimSpace(s.Script)
	out.PathFields = pCommon.NormalizeStrings(s.PathFields) // 去空格、丢空值、去重,保留声明顺序
	if s.Shelled {
		out.ReadOnly = false //命令类工具必须可写
		out.Sandboxed = true // 命令类工具必须进沙箱
	}
	if out.ReadOnly {
		out.Sandboxed = false // 只读的无需沙箱
	}
	return out
}

// IsValid 名称和脚本必须都有效
func (s ToolSpec) IsValid() bool {
	return strings.TrimSpace(s.Name) != pCommon.BlankString &&
		strings.TrimSpace(s.Script) != pCommon.BlankString
}

// Flags 仅仅用于日志和测试诊断
func (s ToolSpec) Flags() string {
	switch {
	case s.ReadOnly:
		return "readonly"
	case s.Sandboxed:
		return "sandboxed"
	default:
		return "direct"
	}
}

// ValidateStringArgs 校验模型返回的参数 JSON,返回解析后的参数对象
// 校验以JSON字符串形式的参数
func (s ToolSpec) ValidateStringArgs(argsJson string) (map[string]any, error) {
	trimmed := strings.TrimSpace(argsJson)
	if trimmed == pCommon.BlankString {
		trimmed = "{}"
	}

	var input map[string]any
	err := json.Unmarshal([]byte(trimmed), &input)
	if err != nil {
		return nil, exception.ToolArgInvalidKind.Newf("args is not a JSON object: %s", err.Error())
	}

	if input == nil {
		input = map[string]any{}
	}

	if err := s.ValidateObjectArgs(input); err != nil {
		return nil, err
	}

	return input, nil
}

// ValidateObjectArgs 只校验已经解析出来的参数对象
func (s ToolSpec) ValidateObjectArgs(argsMap map[string]any) error {
	if len(s.Schema) == 0 {
		return nil // 无schema,不需要做语义校验
	}

	if argsMap == nil {
		argsMap = map[string]any{}
	}

	// 1. required
	props, _ := s.Schema["properties"].(map[string]any)
	for _, name := range pCommon.GetStringList(s.Schema["required"]) {
		if _, ok := argsMap[name]; !ok {
			return exception.ToolArgInvalidKind.Newf("missing required field %q", name)
		}
	}

	// 2. additionalProperties:false -> 拒绝未声明字段(防止模型自造参数)
	allowAdditional := true
	if v, ok := s.Schema["additionalProperties"].(bool); ok {
		allowAdditional = v
	}

	// 3.逐字段 type/enum
	for key, raw := range argsMap {
		propAny, declared := props[key]
		if !declared {
			if !allowAdditional {
				return exception.ToolArgInvalidKind.Newf("unexpected field %q", key)
			}
			continue
		}

		prop, ok := propAny.(map[string]any)
		if !ok {
			continue
		}

		if err := checkFieldValue(key, raw, prop); err != nil {
			return err
		}
	}

	return nil
}

/// =================== private functions & methods for module ===========================

// schema 的 type子集可能是 "string" ,也可能是 ["string","null"]
// integer视为 number的子集
func typeAllows(typeString any, want string) bool {
	match := func(t string) bool {
		if t == want {
			return true
		}

		return t == "number" && want == "integer"
	}

	switch t := typeString.(type) {
	case string:
		return match(t)
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok && match(s) {
				return true
			}
		}
		return false
	default:
		return true // 未声明,不校验
	}
}

func jsonType(v any) string {
	switch n := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64:
		if n == math.Trunc(n) && !math.IsInf(n, 0) {
			return "integer"
		}
		return "number"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	default:
		return fmt.Sprintf("%T", v)
	}
}

func describeType(types any) string {
	switch t := types.(type) {
	case string:
		return t
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			parts = append(parts, fmt.Sprint(e))
		}
		return strings.Join(parts, "|")
	default:
		return fmt.Sprint(t)
	}
}

func checkFieldValue(field string, v any, prop map[string]any) error {
	if v == nil {
		if typeAllows(prop["type"], "null") {
			return nil
		}

		return exception.ToolArgInvalidKind.Newf("field %q must not be null", field)
	}

	if types, ok := prop["type"]; ok {
		actual := jsonType(v)
		if !typeAllows(types, actual) {
			return exception.ToolArgInvalidKind.Newf("field %q must be %s,got %q", field, describeType(types), actual)
		}
	}

	if enum1, ok := prop["enum"].([]any); ok && enum1 != nil {
		hit := false
		for _, e := range enum1 {
			if fmt.Sprint(e) == fmt.Sprint(v) {
				hit = true
				break
			}
		}

		if !hit {
			return exception.ToolArgInvalidKind.Newf("field %q must be one of %v,got %v", field, enum1, v)
		}
	}

	return nil
}
