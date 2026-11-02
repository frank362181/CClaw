/*
*	沙箱接口,是隔离器的后端抽象,不同OS的沙箱均从这里继承(如bubbleWrap,Seatbelt,LandLock等)
**/

package toolexecutor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"my.favorite/cclawservice/harness/security"
	pCommon "my.favorite/platformutility/common"
)

// Cleanup 沙箱的清理函数
type Cleanup func()

type ISandBox interface {
	Name() string    //返回名称
	Available() bool //是否支持

	// Prepare 为给定的命令准备啥想画的*exec.Cmd
	Prepare(ctx context.Context, spec *SandBoxSpec, cmd *exec.Cmd) (*exec.Cmd, Cleanup, error)
}

// ISandboxedBuilder 对原有的沙箱规格进行修改,从工具输入构建原始命令,返回 *exec.Cmd 尚未被沙箱包装
type ISandboxedBuilder interface {
	BuildCommand(ctx context.Context, input map[string]any, cc security.CallContext) (*exec.Cmd, error)
}

// SandBoxSpec 沙箱执行隔离策略
type SandBoxSpec struct {
	Enabled    bool   // 是否真正套沙箱,false=直通执行
	WorkingDir string //工作目录(CWD)

	AllowedDirWrite []string // 额外允许写入的路径列表,相对于WorkingDir的相对路径或绝对路径
	DenyDirRead     []string // 额外禁止读取的路径列表(绝对路径)

	AllowedNetwork bool //是否允许网络访问

	AllowedDomains []string // 允许网络访问下的域名

	ToolName string //触发沙箱的工具名称,用于诊断
}

func BuildSandBoxSpec(env ToolEnv, toolSpec ToolSpec) SandBoxSpec {
	rules := env.ConfiguredRules

	spec := SandBoxSpec{
		ToolName:       toolSpec.Name,
		Enabled:        rules.SandboxEnabled && toolSpec.Sandboxed,
		WorkingDir:     strings.TrimSpace(env.WorkRoot),
		AllowedNetwork: rules.SandboxNetwork,
	}

	// 与ToolConfiguredRules.AdmitRule的WriteRoots保持同一口径
	spec.AllowedDirWrite = rules.writeRoots(env.WorkRoot)

	// denyRoots 里值有绝对路径对沙箱有意义(其余是文件名/后缀)
	for _, dir := range rules.DenyRoots {
		if filepath.IsAbs(dir) {
			spec.DenyDirRead = append(spec.DenyDirRead, dir)
		}
	}

	return spec
}

// Normalize 是对sandbox Specification添加兜底项后的改写
func (spec SandBoxSpec) Normalize() SandBoxSpec {
	out := spec

	// 1. 兜底黑名单 + 调用方追加项
	defDeniedDirs := defaultDeniedReadPaths()
	deniedDirs := make([]string, 0, len(defDeniedDirs)+len(spec.DenyDirRead))
	deniedDirs = append(deniedDirs, defDeniedDirs...)
	for _, dir := range spec.DenyDirRead {
		if filepath.IsAbs(dir) {
			deniedDirs = append(deniedDirs, dir)
		}
	}
	out.DenyDirRead = pCommon.NormalizeStrings(deniedDirs)

	// 2. tempDir自动放行(macOS 上位于 /var/folders,需解析真实路径)
	writeDir := make([]string, 0, len(spec.AllowedDirWrite)+1)
	if tempDir := strings.TrimSpace(os.TempDir()); tempDir != pCommon.BlankString {
		if realTmp := pCommon.ResolveRealPath(tempDir); realTmp != pCommon.BlankString {
			writeDir = append(writeDir, realTmp)
		}
	}
	for _, dir := range spec.AllowedDirWrite {
		if filepath.IsAbs(dir) {
			writeDir = append(writeDir, dir)
		}
	}
	out.AllowedDirWrite = pCommon.NormalizeStrings(writeDir)

	return out
}

func prepareSandboxedCommand(ctx context.Context, spec *SandBoxSpec, cmd *exec.Cmd) (*exec.Cmd, Cleanup, error) {
	if spec == nil || !spec.Enabled {
		return cmd, func() {}, nil
	}

	if strings.TrimSpace(spec.WorkingDir) == pCommon.BlankString {
		return nil, nil, fmt.Errorf("sandbox: tool %s requires a working directory,but WorkRoot is empty(agent workspace may not be initialized)", spec.ToolName)
	}

	sandboxMgr := GetSandboxMgr()
	if !sandboxMgr.Available() {
		return nil, nil, fmt.Errorf("sandbox: tool %s requires an isolated run, but no sandbox backend is available on %s", spec.ToolName, runtime.GOOS)
	}

	normalizedSpec := spec.Normalize()
	box := sandboxMgr.Active()
	wrapped, cleanup, err := box.Prepare(ctx, new(normalizedSpec), cmd)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox: prepare failed for tool %s (backend=%s): %w", spec.ToolName, box.Name(), err)
	}

	if cleanup == nil {
		cleanup = func() {}
	}

	return wrapped, cleanup, nil
}
