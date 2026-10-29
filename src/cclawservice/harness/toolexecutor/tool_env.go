/**
* 工具执行的 **环境值对象**
*
* 一次 RunAll 需要的全部外部依赖,由调用方构造、值语义传参,执行期只读。
**/

package toolexecutor

import (
	"fmt"
	"strings"

	pCommon "my.favorite/platformutility/common"
)

// ToolEnv 一次工具执行所需的环境。
// 只装"执行需要知道的东西",不装轮次/会话/用户等编排层状态。
type ToolEnv struct {
	Resolver IScriptsResolver // 工具名 -> 脚本绝对路径
	WorkRoot string           // agent 工作区根目录(工具相对路径的基准)
}

// ScriptPath 解析工具名 -> 脚本绝对路径。
// 放在值对象上而不是包级函数:它只依赖自身两个字段,调用点也只有执行器内部,
// 顺带把"工具不存在"的三种成因(空名/无resolver/未注册)收敛成一句可读错误。
func (e ToolEnv) ScriptPath(toolName string) (string, error) {
	toolName = strings.TrimSpace(toolName)
	if len(toolName) == 0 {
		return pCommon.BlankString, fmt.Errorf("tool name is empty")
	}

	if e.Resolver == nil {
		return pCommon.BlankString, fmt.Errorf("scripts is empty")
	}

	path, err := e.Resolver.Resolve(toolName)
	if err != nil {
		return pCommon.BlankString, fmt.Errorf("tool script not registered for tool name %s", toolName)
	}

	if path = strings.TrimSpace(path); path == pCommon.BlankString {
		return pCommon.BlankString, fmt.Errorf("tool script not registered for tool name %s", toolName)
	}

	return path, nil
}
