package toolexecutor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"my.favorite/cclawservice/harness/security"
	pCommon "my.favorite/platformutility/common"
)

// buildSeatbeltProfile 构建 SBPL 策略字符串。
// 对应 go-sandbox 中的 validateSeatbeltLiteral 防护——
// 所有插值必须经过转义，防止 profile 字符串注入。
func buildSeatbeltProfile(spec *SandBoxSpec) string {
	var b strings.Builder
	b.WriteString("(version 1)\n")
	b.WriteString("(deny default)\n")

	// 允许基本的系统调用
	b.WriteString("(allow process-exec)\n")
	b.WriteString("(allow process-fork)\n")
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(allow mach-lookup)\n")

	// 文件读取：默认允许大部分读取，但排除 DenyRead
	b.WriteString("(allow file-read*)\n")
	for _, d := range spec.DenyDirRead {
		b.WriteString(fmt.Sprintf("(deny file-read* (subpath \"%s\"))\n", escapeSBPL(d)))
	}

	// 文件写入：只允许 workspace 和 AllowWrite
	b.WriteString("(deny file-write*)\n")
	if spec.WorkingDir != "" {
		b.WriteString(fmt.Sprintf("(allow file-write* (subpath \"%s\"))\n", escapeSBPL(spec.WorkingDir)))
	}
	for _, w := range spec.AllowedDirWrite {
		b.WriteString(fmt.Sprintf("(allow file-write* (subpath \"%s\"))\n", escapeSBPL(w)))
	}

	// 兜底设备: python/subprocess 经常写 dev/null,不会放行会导致莫名其妙的 EACCESS
	b.WriteString("(allow file-write* (literal \"/dev/null\"))\n")
	b.WriteString("(allow file-write* (literal \"/dev/tty\"))\n")

	// 网络
	if spec.AllowedNetwork {
		b.WriteString("(allow network*)\n")
	} else {
		b.WriteString("(deny network*)\n")
	}

	return b.String()
}

// escapeSBCL 转义 SBPL profile 中的字符串字面量。
// 这是必须的安全措施：不转义的路径可能包含双引号或反斜杠，
// 导致 profile 注入漏洞。
func escapeSBPL(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\t", " ")
	return s
}

// 系统兜底的敏感路径黑名单
// 无论如何配置,这些路径都不应该被访问
func defaultDeniedReadPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	return []string{
		filepath.Join(home, ".ssh"),
		filepath.Join(home, ".aws", "credentials"),
		filepath.Join(home, ".config", "gcloud"),
		filepath.Join(home, ".kube", "config"),
		filepath.Join(home, ".docker", "config.json"),
		filepath.Join(home, ".npmrc"),
		filepath.Join(home, ".git-credentials"),
	}
}

func getSandboxSpecFromCallContext(toolName string, cc security.CallContext) *SandBoxSpec {
	spec := &SandBoxSpec{
		Enabled:    enabledSandboxFromContext(cc), // 与调用方判断一致
		ToolName:   toolName,
		WorkingDir: cc.WorkingDir,
	}

	// 1.兜底的黑名单时基础,配置只能追加,不能移除
	spec.DenyDirRead = append([]string{}, defaultDeniedReadPaths()...)
	if cc.SandboxConfig != nil {
		spec.DenyDirRead = append(spec.DenyDirRead, cc.SandboxConfig.DeniedDirRead...)
	}

	// 2.显式配置优先级,否则由权限模式推导
	if cc.SandboxConfig != nil && cc.SandboxConfig.AllowedNetwork != nil {
		spec.AllowedNetwork = *cc.SandboxConfig.AllowedNetwork
	} else {
		spec.AllowedNetwork = inferAllowedNetworkFromMode(cc.Permission)
	}

	// 3.域名白名单来自配置
	if cc.SandboxConfig != nil && spec.AllowedNetwork {
		spec.AllowedDomains = append(spec.AllowedDomains, cc.SandboxConfig.AllowedDomains...)
	}

	return spec
}

func inferAllowedNetworkFromMode(mode security.PermissionMode) bool {
	switch mode {
	case security.PermissionModeBypass:
		return true
	default:
		return false
	}
}

// 根据请求上下文中判断沙箱是否启用
// 显式配置最高优先级;根据permissionMode推导
func enabledSandboxFromContext(cc security.CallContext) bool {
	if cc.SandboxConfig != nil && cc.SandboxConfig.Enabled != nil {
		return *cc.SandboxConfig.Enabled
	}

	switch cc.Permission {
	case security.PermissionModeBypass, security.PermissionModeDefault, security.PermissionModePlan, security.PermissionModeAccept:
		return true
	default:
		return true
	}
}

// getProcessEnv 沙箱开启时追加 PYTHONDONTWRITEBYTECODE=1:python 默认会往脚本目录写 __pycache__,
// 沙箱只允许写 WorkingDir,不关掉字节码缓存会让整次调用以"写入被拒"失败。
func getProcessEnv(workspaceRoot string, sandboxed bool) []string {
	env := pCommon.GetOSEnvironments()
	if workspaceRoot != "" {
		env = append(env, "CCLAW_AGENT_WORKSPACE="+workspaceRoot)
	}

	if sandboxed {
		env = append(env, "PYTHONDONTWRITEBYTECODE=1")
	}

	return env
}
