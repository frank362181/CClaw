/**
* 9️九段式的带有tool调用前的检查,沙箱支持的脚本执行器
*
* 将责任链与工具执行链接起来,是工具函数调用的入口
*
**/

package toolexecutor

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	"my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/harness/security"
	pCommon "my.favorite/platformutility/common"
	pException "my.favorite/platformutility/exception"
)

type ComposedScriptExecutor struct {
	pipeline *security.PermissionPipeline
	toolList map[string]security.IBaseTool //存放注册进来的工具,就是待执行的工具

	SandboxEnabled bool //是否激活了沙箱
	sandboxMgr     *SandBoxMgr
}

var toolExecutor = pCommon.GetSingleton(func() *ComposedScriptExecutor {
	toolExecutor := &ComposedScriptExecutor{
		pipeline: security.GetPermissionPipeline(), // singleton,default permission pipeline
		toolList: make(map[string]security.IBaseTool),

		sandboxMgr:     GetSandboxMgr(), // singleton,
		SandboxEnabled: false,
	}
	return toolExecutor

}).Instance()

func GetExecutor(sandboxEnabled bool) *ComposedScriptExecutor {
	toolExecutor.SandboxEnabled = sandboxEnabled
	return toolExecutor
}

func (e *ComposedScriptExecutor) Register(baseTool security.IBaseTool) {
	if baseTool == nil {
		return
	}

	toolName := strings.ToLower(baseTool.Name())
	if _, ok := e.toolList[toolName]; ok {
		return
	}
	e.toolList[toolName] = baseTool
}

func (e *ComposedScriptExecutor) Execute(ctx context.Context, req *security.ToolCallRequest) (any, error) {
	tmpTool, ok := e.toolList[req.ToolName]
	if !ok {
		return nil, pException.ParameterEmptyErrorEx2("unknown tool %s", req.ToolName)
	}

	//step 1:运行权限管线
	decision, err := e.pipeline.Head.Handle(ctx, req, tmpTool)
	if err != nil {
		return nil, pException.RuntimeErr2("security pipeline error handling tool %s: %s", req.ToolName, err.Error())
	}

	// step 2:根据决策执行
	switch decision.Behavior {
	case security.Allow:
		// 权限 pipeline 放行 -> 执行工具
	case security.Ask:
		// Ask决策意味着 InterActiveConfirmHandle 之前就返回了
		// 在实际系统中,这里应处罚用户确认流程,但 InteractiveConfirmHandle 已经作为链尾处理了Ask情况
		return nil, exception.PermissionDeniedErrEx2("ask decision not resolved:tool name=%s,reason=%s", req.ToolName, decision.Reason)
	case security.Deny:
		return nil, exception.PermissionDeniedErrEx2("denied by %s,reason:%s", req.ToolName, decision.Reason)
	default:
		return nil, pException.RuntimeErr2("unexpected decision %s", decision.Behavior)
	}

	// 3.Behavior == Allow时跳到这里执行
	if e.enabledSandbox(req, decision) {
		return e.executeSandboxed(ctx, req, tmpTool)
	}

	// 4.无沙箱
	return tmpTool.Call(ctx, req.Input, req.Context)
}

func (e *ComposedScriptExecutor) enabledSandbox(req *security.ToolCallRequest, d *security.Decision) bool {
	if !e.SandboxEnabled {
		return false
	}

	//只读工具跳过沙箱
	if tmpTool, ok := e.toolList[req.ToolName]; ok {
		if tmpTool.IsReadOnly(req.Input) {
			return false
		}
	}

	if !enabledSandboxFromContext(req.Context) {
		return false
	}

	if !e.sandboxMgr.Available() {
		return false
	}

	// 沙箱仅仅覆盖shell等命令
	switch req.ToolName {
	case "Bash", "Shell", "PowerShell", "Monitor":
		return true
	default:
		return false
	}
}

func (e *ComposedScriptExecutor) executeSandboxed(ctx context.Context, req *security.ToolCallRequest, baseTool security.IBaseTool) (any, error) {
	// 如果实现了ToolBuilder接口
	cb, ok := baseTool.(ISandboxedBuilder)
	if !ok {
		//fail-closed,此时须沙箱化
		return nil, exception.SandboxExecutorErrEx2("tool %s requires sandbox but not implement IToolBuilder", req.ToolName)
	}

	cmd, err := cb.BuildCommand(ctx, req.Input, req.Context)
	if err != nil {
		return nil, exception.SandboxExecutorErrEx2("sandbox build command failed: %s", err.Error())
	}

	//构建沙箱环境
	spec := getSandboxSpecFromCallContext(req.ToolName, req.Context)

	// 如果实现了自定义的沙箱策略
	if sa, ok := baseTool.(ISandboxSpecAware); ok {
		spec = sa.SandboxSpec(ctx, req.Input, req.Context)
	}

	// 准备沙箱
	activeBox := e.sandboxMgr.SelectSandbox(req.Context)
	wrapped, cleanup, err := activeBox.Prepare(ctx, spec, cmd)
	if err != nil {
		// 沙箱准备失败
		return nil, exception.SandboxExecutorErrEx2("sandbox prepare command failed: %s", err.Error())
	}
	defer cleanup()

	// 执行沙箱化命令
	output, err := wrapped.CombinedOutput()
	if err != nil {
		// 如果退出码为0,但CombineOutput返回错误,可能只是Stderr 输出
		if exitErr, ok2 := errors.AsType[*exec.ExitError](err); ok2 {
			return map[string]any{
				"stdout":    string(output),
				"exitCode":  exitErr.ExitCode(),
				"sandboxed": true,
				"sandbox":   activeBox.Name(),
			}, nil
		}

		return nil, exception.SandboxExecutorErrEx2("sandboxed executing failed, tool %s: reason:%s", req.ToolName, err.Error())
	}

	return map[string]any{
		"stdout":    string(output),
		"exitCode":  0,
		"sandboxed": true,
		"sandbox":   activeBox.Name(),
	}, nil
}
