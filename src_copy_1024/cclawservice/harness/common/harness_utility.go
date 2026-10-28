/**
* harness包的工具函数
**/

package common

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"my.favorite/cclawservice/harness/api/llmproto"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
	pException "my.favorite/platformutility/exception"
)

// BuildAgentBootstrapRoot 根据agent的信息构建agent的工作目录
// 安全:agent_dir 必须为合法相对路径,防止工作区根逃出 <exeDir>/workspace。
func BuildAgentBootstrapRoot(agent dataobject.TAgent) (string, bool) {
	if !pCommon.IsSafeDir(agent.AgentDir) {
		return pCommon.BlankString, false
	}

	agentBootstrapRoot := filepath.Join(pCommon.GetExecutableDir(), AgentBootstrapDir, agent.OwnerId, agent.AgentDir)
	if pCommon.IsDir(agentBootstrapRoot) {
		return agentBootstrapRoot, true
	}

	return pCommon.BlankString, false
}

// ResolveStartupPath 校验并解析启动文档路径:仅允许纯文件名,禁止目录穿越与符号链接逃逸
// 供 prompt 组装(LoadWorkspace)与启动文件管理(StartupFileRepository)共用,
// 保证同一套安全路径规则。文件名非法/越界时返回平台 FilePathError。
func ResolveStartupPath(workspaceRoot, fileName string) (string, error) {
	// 只允许纯文件名:不能包含路径分隔符、"." 或 ".."
	if filepath.Base(fileName) != fileName || fileName == "." || fileName == ".." {
		return pCommon.BlankString, pException.FilePathErrorEx("illegal document file name: " + fileName)
	}

	return pCommon.SafeJoin(workspaceRoot, filepath.Join(workspaceRoot, fileName))
}

// FindStartupDocFile 按文件名(精确匹配)在启动文件清单中查找,未命中返回 ok=false。
func FindStartupDocFile(fileName string) (StartupDocSpec, bool) {
	for _, f := range StartupDocFiles {
		if f.Name == fileName {
			return f, true
		}
	}
	return StartupDocSpec{}, false
}

func BasicToolToLLMTool(t BasicTool) (llmproto.Tool, bool) {
	name := strings.TrimSpace(t.Name)
	if len(name) == 0 {
		return llmproto.Tool{}, false
	}

	return llmproto.Tool{
		Type: "function",
		Function: llmproto.ToolFunction{
			Name:        name,
			Description: strings.TrimSpace(t.Description),
			Parameters:  pCommon.YamlToJsonMap(t.Parameters),
		},
	}, true
}

// StreamReadError 归一化流读取失败:
// 流式读取失败的归一化:把底层读流错误分类为"客户端断开"或"传输层故障"。
//   - 客户端断开(外层 ctx 取消)      → ErrRequestCanceled(静默,不重试)
//   - 空闲超时(仅内层 streamCtx 取消) → 传输层错误(可重试)
//   - 其它(连接重置/EOF 等)          → 传输层错误(可重试)
func StreamReadError(ctx context.Context, streamCtx context.Context, err error, requestID string, idle time.Duration) error {
	if ctx.Err() != nil {
		return llmproto.ErrRequestCanceled
	}
	if idle > 0 && streamCtx.Err() != nil {
		return llmproto.NewLlmStreamTransportError(
			fmt.Errorf("stream idle timeout after %s without data: %v", idle, err), requestID)
	}
	if pException.IsContextCanceled(streamCtx, err) {
		return llmproto.ErrRequestCanceled
	}
	return llmproto.NewLlmStreamTransportError(err, requestID)
}
