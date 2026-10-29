/**
* 一轮对话内 **N 次工具调用** 的调度策略
*
* 与同包另外两个概念的层次区别(避免同名混淆):
*   - ToolExecutor(实现 llmproto.IToolScriptRunner): **一次** 脚本调用怎么跑(进程/沙箱/权限)
*   - IToolRoundExecutor(本文件): **一轮 N 次** 调用怎么调度(串行/并行 + 进度汇集)
*   - chat.ToolLoop: **多轮** 循环 + 结果回填
**/

package toolexecutor

import (
	"context"
	"os/exec"

	"my.favorite/cclawservice/harness/security"
)

type SystemToolName string

const (
	SystemToolGlob  SystemToolName = "Glob"  //搜索路径
	SystemToolGrep  SystemToolName = "Grep"  //搜索文件内容
	SystemToolRead  SystemToolName = "Read"  //读取
	SystemToolWrite SystemToolName = "Write" //覆盖或创建
	SystemToolEdit  SystemToolName = "Edit"  //replace
	SystemToolBash  SystemToolName = "Bash"  //执行
)

func (s SystemToolName) String() string {
	return string(s)
}

// IToolRoundExecutor 工具执行策略:同步接口串行、流式接口同轮并行 + 进度汇集
// 这是 runToolLoop 与 streamToolLoop 之间唯一的差异,故抽成可替换的策略对象。
type IToolRoundExecutor interface {
	// RunAll 执行本轮模型声明的全部工具调用
	// 契约:
	//   - 返回的 outcome **顺序**必须与 invocations 一致
	//   - 返回条数 <= len(invocations):只有 ctx 已取消时才能缺条
	RunAll(ctx context.Context, env ToolEnv, invocations []ToolInvocation,
		sink ToolEventSink, ledger ToolLedger) []ToolOutcome
}

type ISandboxedBuilder interface {
	// BuildCommand  沙箱化包装接口,从工具输入构建原始命令,返回 *exec.Cmd 尚未被沙箱包装
	BuildCommand(ctx context.Context, input map[string]any, cc security.CallContext) (*exec.Cmd, error)
}
