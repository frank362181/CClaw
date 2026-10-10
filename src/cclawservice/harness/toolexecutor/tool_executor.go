/*
* 将责任链与工具执行链接起来,是工具函数调用的入口
**/

package toolexecutor

import (
	"context"

	"my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/harness/security"
)

type Executor struct {
	pipeline *security.PermissionPipeline
	tools    map[string]security.ITool
}

func NewExecutor(pipeline *security.PermissionPipeline) *Executor {
	return &Executor{
		pipeline: pipeline,
		tools:    make(map[string]security.ITool),
	}
}

func (e *Executor) Register(tool security.ITool) {
	e.tools[tool.Name()] = tool
}

func (e *Executor) Execute(ctx context.Context, req *security.ToolCallRequest) (any, error) {
	tool, ok := e.tools[req.ToolName]
	if !ok {
		return nil, exception.PermissionDeniedErrEx2("unknown tool %s", req.ToolName)
	}

	//step 1:运行权限管线
	decision, err := e.pipeline.Head.Handle(ctx, req, tool)
	if err != nil {
		return nil, exception.PermissionDeniedErrEx2("error handling tool %s: %s", req.ToolName, err.Error())
	}

	// step 2:根据决策执行
	switch decision.Behavior {
	case security.Allow:
		// 管线放行 -> 执行工具
		return tool.Call(ctx, req.Input, req.Context)
	case security.Ask:
		// Ask决策意味着 InterActiveConfirmHandle 之前就返回了
		// 在实际系统中,这里应处罚用户确认流程,但 InteractiveConfirmHandle 已经作为链尾处理了Ask情况
		return nil, exception.PermissionDeniedErrEx2("unexpected ask decision at executor level:tool name=%s,reason=%s", req.ToolName, decision.Reason)
	case security.Deny:
		return nil, exception.PermissionDeniedErrEx2("denied by %s,reason:%s", req.ToolName, decision.Reason)
	default:
		return nil, exception.PermissionDeniedErrEx2("unknown decision behavior %s", decision.Behavior)
	}
}
