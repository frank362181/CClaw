/*
*	沙箱接口,是隔离器的后端抽象,不同OS的沙箱均从这里继承(如bubbleWrap,Seatbelt,LandLock等)
**/

package toolexecutor

import (
	"context"
	"os/exec"
)

// Cleanup 沙箱的清理函数
type Cleanup func()

type ISandBox interface {
	Name() string    //返回名称
	Available() bool //是否支持

	// Prepare 为给定的命令准备啥想画的*exec.Cmd
	Prepare(ctx context.Context, spec *SandBoxSpec, cmd *exec.Cmd) (*exec.Cmd, Cleanup, error)
}

// SandBoxSpec 沙箱执行隔离策略
type SandBoxSpec struct {
	WorkingDir string //工作目录

	AllowedDirWrite []string //允许写入的路径列表,相对于WorkingDir的相对路径或绝对路径
	DenyDirRead     []string // 禁止读取的路径列表

	AllowedNetwork bool //是否允许网络访问

	AllowedDomains []string // 允许网络访问下的域名

	ToolName string //触发沙箱的工具名称,用于诊断
}
