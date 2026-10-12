/*
* 沙箱化工具接口类
*	该接口支持将工具转换为可被沙箱包装的 *exec.Cmd,且只有实现了该接口的工具才能被沙箱化
**/

package tool

import (
	"context"
	"os/exec"

	"my.favorite/cclawservice/harness/security"
)

type IToolBuilder interface {
	// BuildCommand 从工具输入构建原始命令,返回 *exec.Cmd 尚未被沙箱包装
	BuildCommand(ctx context.Context, input map[string]any, cc security.CallContext) (*exec.Cmd, error)
}
