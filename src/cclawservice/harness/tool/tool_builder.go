/*
* 给工具函数加上额外的包装的接口类
**/

package tool

import (
	"context"
	"os/exec"

	"my.favorite/cclawservice/harness/security"
)

type ISandboxedBuilder interface {
	// BuildCommand  沙箱化包装接口,从工具输入构建原始命令,返回 *exec.Cmd 尚未被沙箱包装
	BuildCommand(ctx context.Context, input map[string]any, cc security.CallContext) (*exec.Cmd, error)
}
