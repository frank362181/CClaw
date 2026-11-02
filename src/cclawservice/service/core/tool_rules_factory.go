/**
* 从配置文件获取工具执行时的配置的安全规则
*
* 放在service模块的原因:这里需要读配置文件,toolExecutor则只是应用配置信息
**/

package core

import (
	"path/filepath"
	"strings"

	"my.favorite/cclawservice/harness/toolexecutor"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/config"
	"my.favorite/platformutility/logging"
)

const (
	ToolConfiguredRuleSection = "toolRule"

	ToolConfiguredRuleKeyEnabled         = "policyEnabled"
	ToolConfiguredRuleKeyFunctionCalling = "functionCalling"
	ToolConfiguredRuleKeyAllowShell      = "allowShell"
	ToolConfiguredRuleKeySandbox         = "sandboxEnabled"
	ToolConfiguredRuleSandboxNetwork     = "sandboxNetwork"
	ToolConfiguredRuleKeyAutoSandboxed   = "autoAllowBashIfSandboxed"
	ToolConfiguredRuleKeyReadRoots       = "readRoots"
	ToolConfiguredRuleKeyWriteRoots      = "writeRoots"
	ToolConfiguredRuleKeyDenyRoots       = "denyRoots"
)

func LoadToolConfiguredRules(helper *config.ConfigurationHelper) toolexecutor.ToolConfigRules {
	rules := toolexecutor.ToolConfiguredRuleZero

	if helper == nil {
		logging.LogWarnF("tool rules: empty rules in config file,policy disabled")
		return rules
	}

	rules.PolicyEnabled, _ = helper.GetBoolValueDefault(ToolConfiguredRuleSection, ToolConfiguredRuleKeyEnabled, false)
	rules.AllowShell, _ = helper.GetBoolValueDefault(ToolConfiguredRuleSection, ToolConfiguredRuleKeyAllowShell, true)
	rules.SandboxEnabled, _ = helper.GetBoolValueDefault(ToolConfiguredRuleSection, ToolConfiguredRuleKeySandbox, false)
	rules.SandboxNetwork, _ = helper.GetBoolValueDefault(ToolConfiguredRuleSection, ToolConfiguredRuleSandboxNetwork, true)
	rules.AutoAllowBashIfSandboxed, _ = helper.GetBoolValueDefault(ToolConfiguredRuleSection, ToolConfiguredRuleKeyAutoSandboxed, false)

	rawReadRoots, _ := helper.GetStringValueDefault(ToolConfiguredRuleSection, ToolConfiguredRuleKeyReadRoots, pCommon.BlankString)
	rawWriteRoots, _ := helper.GetStringValueDefault(ToolConfiguredRuleSection, ToolConfiguredRuleKeyWriteRoots, pCommon.BlankString)
	rawDenyRoots, _ := helper.GetStringValueDefault(ToolConfiguredRuleSection, ToolConfiguredRuleKeyDenyRoots, pCommon.BlankString)

	rules.ReadRoots = normalizeAbsoluteRoots(ToolConfiguredRuleKeyReadRoots, pCommon.SplitStringEx(rawReadRoots, ","))
	rules.WriteRoots = normalizeAbsoluteRoots(ToolConfiguredRuleKeyWriteRoots, pCommon.SplitStringEx(rawWriteRoots, ","))
	rules.DenyRoots = normalizeDenyRoots(pCommon.SplitStringEx(rawDenyRoots, ","))

	if rules.AutoAllowBashIfSandboxed && (!rules.SandboxEnabled || !toolexecutor.GetSandboxMgr().Available()) {
		logging.LogWarnF("tool rules: autoAllowBashIfSandboxed=true has NO effect " +
			"(requires sandboxEnabled=true AND an available backend); bash still follows allowShell")
	}

	logging.LogInfoF("tool rules: enabled=%v allowShell=%v sandboxEnabled=%v sandboxNetwork=%v",
		rules.PolicyEnabled, rules.AllowShell, rules.SandboxEnabled, rules.SandboxNetwork)

	return rules
}

// readRoots / writeRoots 仅仅接受绝对路径
// 相对路径到底是进程CWD,还是exe所在目录,还是其他的,歧义太大
func normalizeAbsoluteRoots(key string, rootDirs []string) []string {
	dirItems := pCommon.NormalizeStrings(rootDirs)
	if len(dirItems) == 0 {
		return nil
	}

	out := make([]string, 0, len(rootDirs))
	for _, rootDir := range dirItems {
		root := pCommon.NormalizePath(rootDir, pCommon.BlankString, true, true, false, true) // 展开 ~
		if root == pCommon.BlankString || !filepath.IsAbs(root) {
			logging.LogWarnF("tool rules: [tools] %s entry %q dropped (must be an absolute path)", key, rootDir)
			continue
		}

		if realRoot := pCommon.ResolveRealPath(filepath.Clean(root)); realRoot != pCommon.BlankString {
			root = realRoot
		}

		out = append(out, root)
	}

	if len(out) == 0 {
		logging.LogWarnF("tool rules: [tools] %s has entries but none usable, treated as empty", key)
		return nil
	}

	return out
}

// 支持两种写法(与 ToolConfigRules.DenyRoots 注释一致)
//   - 绝对路径(含 ~ 开头):目录前缀 -> 解析成真是路径
//   - 其他(.env / .pem / id_rsa):文件名或后缀 -> 保留原样,但统一小写
//
// 统一小写的原因macOS/Windows大小写不敏感
func normalizeDenyRoots(items []string) []string {
	tempItems := pCommon.NormalizeStrings(items)
	if len(tempItems) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, 4)
	out := make([]string, 0, len(tempItems))
	for _, item := range tempItems {
		p := pCommon.NormalizePath(item, pCommon.BlankString, true, true, false, true)
		if p == pCommon.BlankString {
			continue
		}

		if filepath.IsAbs(p) {
			if realRoot := pCommon.ResolveRealPath(filepath.Clean(p)); realRoot != pCommon.BlankString {
				p = realRoot
			}
		} else {
			p = strings.ToLower(p)
		}

		if _, dup := seen[p]; dup {
			continue
		}

		seen[p] = struct{}{}
		out = append(out, p)
	}

	if len(out) == 0 {
		logging.LogWarnF("tool rules: denyRoots has entries but none usable, treated as empty")
		return nil
	}

	return out
}
