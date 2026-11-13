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
	"sync/atomic"
	"time"
)

type SystemToolName string

const (
	SystemToolGlob  SystemToolName = "glob"  //搜索路径
	SystemToolGrep  SystemToolName = "grep"  //搜索文件内容
	SystemToolRead  SystemToolName = "read"  //读取
	SystemToolWrite SystemToolName = "write" //覆盖或创建
	SystemToolEdit  SystemToolName = "edit"  //replace
	SystemToolBash  SystemToolName = "bash"  //执行
)

func (s SystemToolName) String() string {
	return string(s)
}

const (
	EnvToolRoundTimeoutName     = "CCLAW_TOOL_ROUND_TIMEOUT"
	EnvToolRoundWaitTimeoutName = "CCLAW_TOOL_WAIT_TIMEOUT"

	DefaultToolRoundTimeout = 60 // 工具调用正常收敛时长
	DefaultToolWaitTimeout  = 5  // 工具调用取消的上限时长

	scriptBin          = "python3"         // 脚本解释器(与 SKILL.md requires.bins 对齐)
	scriptRunTimeout   = 120 * time.Second // 单次脚本执行默认超时(OCR/大目录耗时,可用 CCLAW_SCRIPT_TIMEOUT_SEC 覆盖)
	scriptMaxOutput    = 256 * 1024        // stdout/stderr 各 256KB 上限
	scriptMaxInputJSON = 64 * 1024         // 传入参数 JSON 上限
)

// IToolRoundExecutor 工具执行策略:同步接口串行、流式接口同轮并行 + 进度汇集
// 这是 runToolLoop 与 streamToolLoop 之间唯一的差异,故抽成可替换的策略对象。
type IToolRoundExecutor interface {
	// RunAll 执行本轮模型声明的全部工具调用
	// 契约:
	//   - 返回的 outcome **顺序**必须与 invocations 一致
	//   - 返回条数 <= len(invocations):只有 ctx 已取消时才能缺条
	RunAll(ctx context.Context, env ToolEnv, invocations []ToolInvocation, sink ToolEventSink, ledger ToolLedger) []ToolOutcome
}

// ScriptArgument 工具脚本执行的参数
type ScriptArgument struct {
	Path     string // 脚本绝对路径
	Args     string //模型给出的JSON参数,原样作为脚本入参
	WorkRoot string // 脚本的工作目录

	// 非nil表示调用方消费进度:实现应走流式执行,逐行回调 stdout + stderr;
	// nil表示无消费者,实现可走缓冲路径(省掉逐行回调与管道编排的开销)
	OnLine func(line string)

	// 沙箱规格,零值(Enabled == false) = 不套沙箱
	Sandbox SandBoxSpec // 沙箱规格
}

// IScriptRunner 执行一个工具脚本
// 只暴露一个 **方法
type IScriptRunner interface {
	Run(ctx context.Context, toolArgs ScriptArgument) (string, error)
}

// 工具调用收敛口径,单次执行脚本必须有序 120s <= 工具轮收敛 <= 回合超时

type ToolRoundTiming struct {
	RoundTimeout time.Duration // 正常路径上的收敛上限
	WaitTimeout  time.Duration // 调用方取消的快速收敛
}

var RoundTimingOverride atomic.Pointer[ToolRoundTiming]

func SetToolRoundTiming(t ToolRoundTiming) {
	RoundTimingOverride.Store(&t)
}

func GetToolRoundTiming() ToolRoundTiming {
	t := ToolRoundTiming{}
	if o := RoundTimingOverride.Load(); o != nil {
		t = *o
	}

	return normalizeToolRoundTiming(t)
}
