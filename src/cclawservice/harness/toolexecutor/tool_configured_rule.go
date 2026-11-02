/**
* 定义工具执行的 **声明式** 安全策略
* 零值 {} = 不限制
**/

package toolexecutor

import (
	"fmt"
	"path/filepath"
	"strings"

	"my.favorite/cclawservice/exception"
	pCommon "my.favorite/platformutility/common"
)

// ToolConfigRules 工具执行的声明式的安全策略
type ToolConfigRules struct {
	PolicyEnabled            bool     // false:表示无安全策略,全部放行
	AllowShell               bool     // false:决绝bash类无路径信息的命令执行工具
	SandboxEnabled           bool     //是否开启沙箱环境
	SandboxNetwork           bool     // 沙箱内是否允许访问网络,default=true
	AutoAllowBashIfSandboxed bool     // 如果开启了sandbox,只读工具是否自动放行
	ReadRoots                []string // 读类工具允许读取的根(空 = 不限制,沿用read.py现有的定义)
	WriteRoots               []string // 写类工具允许的落地的根(空 = ToolEnv.WorkRoot)
	DenyRoots                []string // 绝对禁止的路径前缀 / 文件名 / 后缀(任意工具都生效)
}

// ToolConfiguredRuleZero 空规则,0 值
var ToolConfiguredRuleZero = ToolConfigRules{}

type Permission struct {
	Allow  bool
	Args   string //策略规范化后的参数,空串表示未改写
	Rule   string
	Reason string

	Cause error // 直接透传原始错误,非nil时优先返回,避免参数和规则的错配
}

var (
	allowAll = Permission{Allow: true, Rule: "allowed-all"}
)

func (p Permission) Arguments(fallbackArgs string) string {
	if p.Args == pCommon.BlankString {
		return fallbackArgs
	}

	// 保持原样
	return p.Args
}

// 将拒绝结果转换成error,便于回填给模型
func (p Permission) Error(toolName string) error {
	if p.Cause != nil {
		return p.Cause
	}

	if p.Reason == pCommon.BlankString {
		return exception.ToolExecutionConfigKind.Newf("tool %s denied by configured rules(rule=%s)", toolName, p.Rule)
	}

	return exception.ToolExecutionConfigKind.Newf("tool %s denied by configured rules(rule=%s),reason=%s", toolName, p.Rule, p.Reason)
}

// AdmitRule 每次工具调用都要检查,纯函数调用,无副作用,幂等
func (c ToolConfigRules) AdmitRule(env ToolEnv, invocation ToolInvocation) Permission {
	if !c.PolicyEnabled {
		return allowAll
	}

	// 1. unknown-tool
	spec, err := env.Spec(invocation.Name())
	if err != nil {
		return denyRule("unknown-tool", "%s", err.Error())
	}

	// 2. invalid args
	input, err := spec.ValidateStringArgs(invocation.Args())
	if err != nil {
		return denyCause("invalid-args", err)
	}

	// 3~5. 工具对应的脚本路径是否生效
	if denied := c.admitPath(env, spec, input); !denied.Allow {
		return denied
	}

	// 6.shell 规则
	return c.admitShell(spec)
}

func (c ToolConfigRules) admitPath(env ToolEnv, spec ToolSpec, input map[string]any) Permission {
	rawPaths := extractToolPath(spec, input)
	if len(rawPaths) == 0 {
		return allowAll
	}

	resolved := make([]string, 0, len(rawPaths))
	for _, rawPath := range rawPaths {
		realPath, err := pCommon.ResolvePath(rawPath, env.WorkRoot)
		if err != nil {
			return denyRule("invalid-path", "%s", err.Error())
		}
		resolved = append(resolved, realPath)
	}

	// deny-path -- 最高优先级,任何工具、模式都不例外
	if hit, pattern := matchDenyRoot(resolved, c.DenyRoots); hit {
		return denyRule("deny-path", "路径命中规则:%s", pattern)
	}

	// 分级包含性
	if spec.ReadOnly {
		// 读类
		if len(c.ReadRoots) > 0 && !allToolWithinRoots(resolved, c.ReadRoots) {
			return denyRule("read-outside-root", "读取路径不在允许的根内")
		}

		return allowAll
	}

	// 写类,WorkRoots为空时退化为WorkRoot
	roots := c.writeRoots(env.WorkRoot)
	if len(roots) == 0 {
		return denyRule("no-write-root", "写入根路径为配置,且Agent工作区未初始化")
	}

	if !allToolWithinRoots(resolved, roots) {
		return denyRule("write-oudside-root", "写入路径 %s 不在允许的根内(允许:%s)", strings.Join(resolved, ","), strings.Join(roots, ","))
	}

	return allowAll
}

func (c ToolConfigRules) admitShell(spec ToolSpec) Permission {
	// shell-disabled
	if c.AllowShell || !spec.Shelled {
		return allowAll
	}

	// 允许 shell,且强制沙箱:只有真的能隔离才放行;否则将会更加危险
	if c.AutoAllowBashIfSandboxed && c.SandboxEnabled && GetSandboxMgr().Available() {
		return allowAll
	}

	return denyRule("shell-disabled", "命令执行类工具已经被禁用(allowShell=false)")
}

// =============================== private functions & methods for module & class =====================

// 返回写类工具允许的根:agent 工作去 应该包含在内,配置只能追加

func (c ToolConfigRules) writeRoots(workRoot string) []string {
	roots := make([]string, 0, len(c.WriteRoots)+1)
	if root := strings.TrimSpace(workRoot); root != pCommon.BlankString {
		roots = append(roots, root)
	}

	for _, root := range c.WriteRoots {
		root = strings.TrimSpace(root)
		if root == pCommon.BlankString || !filepath.IsAbs(root) {
			continue // 相对路径歧义太大
		}

		roots = append(roots, root)
	}

	return pCommon.NormalizeStrings(roots)
}

// 构造一条拒绝的规则
func denyRule(rule, format string, args ...any) Permission {
	return Permission{Rule: rule, Reason: fmt.Sprintf(format, args...)}
}

func denyCause(rule string, cause error) Permission {
	return Permission{Rule: rule, Reason: cause.Error(), Cause: cause}
}
