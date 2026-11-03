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

	ConfiguredRules ToolConfigRules // 执行的安全配置策略

	// 本轮唯一的标识,用作goroutine的作用域名称
	// 空表示未提供,执行器会退化为"tool-turn"
	ScopeName string
}

func NewToolEnv(resolver IScriptsResolver, workRoot string, scope string, rules ToolConfigRules) ToolEnv {
	return ToolEnv{
		Resolver:        resolver,
		WorkRoot:        workRoot,
		ConfiguredRules: rules,
		ScopeName:       scope,
	}
}

// Spec 解析工具名 -> 执行期元数据
// 三种不可用工具的成因(空名 / 无 resolver / 未注册 / 无脚本)
func (e ToolEnv) Spec(toolName string) (ToolSpec, error) {
	toolName = strings.TrimSpace(toolName)
	if len(toolName) == 0 {
		return ToolSpec{}, fmt.Errorf("tool name is empty")
	}

	if e.Resolver == nil {
		return ToolSpec{}, fmt.Errorf("tool %s:scripts is empty", toolName)
	}

	spec, ok := e.Resolver.Resolve(toolName)
	if !ok {
		return ToolSpec{}, fmt.Errorf("tool script not registered for tool name %s", toolName)
	}

	spec = spec.Normalize()
	if !spec.IsValid() {
		return ToolSpec{}, fmt.Errorf("tool script not registered for tool name %s", toolName)
	}

	return spec, nil
}

// ScriptPath 只要路径的便捷入口函数
func (e ToolEnv) ScriptPath(toolName string) (string, error) {
	spec, err := e.Spec(toolName)
	if err != nil {
		return pCommon.BlankString, err
	}

	return spec.Script, nil
}

func (e ToolEnv) AdmitRule(invocation ToolInvocation) Permission {
	return e.ConfiguredRules.AdmitRule(e, invocation)
}
