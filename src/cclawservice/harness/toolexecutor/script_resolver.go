/**
* 工具名 -> 脚本绝对路径 的 **只读索引**
*
* 本类中采用一个不可变的数据、可打印、可断言的数据类型;
*
**/

package toolexecutor

import (
	"strings"

	pCommon "my.favorite/platformutility/common"
)

// IScriptsResolver 工具名 -> 工具脚本绝对路径的解析。
// 只暴露读侧(执行期不可写),由本包的 ScriptsResolver 提供默认实现;
// 调用方(如 chat.TurnState)只依赖这个接口,不感知底层是 map 还是别的索引
type IScriptsResolver interface {
	Resolve(toolName string) (ToolSpec, bool)
}

type ScriptsResolver map[string]ToolSpec

func GetScriptsResolver(specs map[string]ToolSpec) IScriptsResolver {
	out := make(ScriptsResolver, len(specs))
	for key, raw := range specs {
		spec := raw.Normalize()
		if spec.Name == pCommon.BlankString {
			spec.Name = strings.TrimSpace(strings.TrimSpace(key))
		}

		if !spec.IsValid() {
			continue
		}

		if _, dup := out[spec.Name]; dup {
			continue
		}

		out[spec.Name] = spec
	}

	return out
}

func (r ScriptsResolver) Resolve(toolName string) (ToolSpec, bool) {
	key := strings.ToLower(strings.TrimSpace(toolName))
	if key == pCommon.BlankString {
		return ToolSpec{}, false
	}

	toolSpec, ok := r[key] // nil map读取安全:返回零值
	if !ok {
		return ToolSpec{}, false
	}

	return toolSpec, true
}

// Names 已经注册的工具名排序,用于日志与测试断言
func (r ScriptsResolver) Names() []string {
	out := make([]string, 0, len(r))
	for name := range r {
		out = append(out, name)
	}

	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}

	return out
}

var (
	_ IScriptsResolver = (ScriptsResolver)(nil)
	_ IScriptsResolver = (*ScriptsResolver)(nil)
)
