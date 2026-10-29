/**
* 工具名 -> 脚本绝对路径 的 **只读索引**
*
* 本类中采用一个不可变的数据、可打印、可断言的数据类型;
*
**/

package toolexecutor

import (
	"fmt"
	"strings"

	pCommon "my.favorite/platformutility/common"
)

// IScriptsResolver 工具名 -> 工具脚本绝对路径的解析。
// 只暴露读侧(执行期不可写),由本包的 ScriptsResolver 提供默认实现;
// 调用方(如 chat.TurnState)只依赖这个接口,不感知底层是 map 还是别的索引
type IScriptsResolver interface {
	Resolve(toolName string) (string, error)
}

type ScriptsResolver map[string]string

func GetScriptsResolver(scripts map[string]string) IScriptsResolver {
	out := make(ScriptsResolver, len(scripts))
	for name, path := range scripts {
		key := strings.TrimSpace(name)
		if key == pCommon.BlankString {
			continue
		}

		if p := strings.TrimSpace(path); p != pCommon.BlankString {
			out[key] = p
		}
	}

	return out
}

func (r ScriptsResolver) Resolve(toolName string) (string, error) {
	toolName = strings.TrimSpace(toolName)
	if toolName == pCommon.BlankString {
		return pCommon.BlankString, fmt.Errorf("blank string")
	}

	path := strings.TrimSpace(r[toolName]) // nil map读取安全:返回零值
	if path == pCommon.BlankString {
		return pCommon.BlankString, fmt.Errorf("tool %s: no script registered", toolName)
	}

	return path, nil
}

var (
	_ IScriptsResolver = (ScriptsResolver)(nil)
	_ IScriptsResolver = (*ScriptsResolver)(nil)
)
