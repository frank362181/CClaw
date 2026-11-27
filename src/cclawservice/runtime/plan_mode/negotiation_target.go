/**
* 通过用户question被识别出来的Entity对象,以及对象对应的属性
*
* 设计边界:
* - 一次请求只产出一个计划、一个主目标 -> 执行器 0 改动
* - 其余目标只用于"让人看见":计划预览 / 审批页 / 留痕
* - 是否阻塞执行只由"有没有至少一个可用目标"决定,与目标总数无关
**/

package plan_mode

import (
	"path/filepath"
	"strings"

	pCommon "my.favorite/platformutility/common"
)

// NegotiationTarget 最终的计划执行是围绕着project_dir进行的
type NegotiationTarget struct {
	Name       string `json:"name"` // 目标名
	ProjectDir string `json:"project_dir,omitempty"`
	Found      bool   `json:"found"`             // 是否有对应的材料
	Primary    bool   `json:"primary,omitempty"` // 主目标
	Reason     string `json:"reason,omitempty"`  // 未命中/ 未执行的原因说明
}

// UsableTargets 返回有用的目标,即有材料
func UsableTargets(targets []NegotiationTarget) []NegotiationTarget {
	if len(targets) == 0 {
		return nil
	}

	usable := make([]NegotiationTarget, 0, len(targets))
	for _, target := range targets {
		if target.Found && strings.TrimSpace(target.ProjectDir) != pCommon.BlankString {
			usable = append(usable, target)
		}
	}

	return usable
}

// MarkPrimaryTarget 标记主目标(与spec.project_dir一致即是)
// 审批页能回答本地要跑哪个
func MarkPrimaryTarget(targets []NegotiationTarget, projectDir string) []NegotiationTarget {
	primaryDir := filepath.Clean(strings.TrimSpace(projectDir))
	if len(targets) == 0 && primaryDir == "" {
		return nil
	}

	out := make([]NegotiationTarget, 0, len(targets)+1)
	out = append(out, targets...)

	if primaryDir == "." || primaryDir == pCommon.BlankString {
		return out
	}

	for i := range out {
		dir := strings.TrimSpace(out[i].ProjectDir)
		if dir == pCommon.BlankString {
			continue
		}

		if filepath.Clean(dir) == projectDir {
			out[i].Found, out[i].Primary = true, true
			return out
		}
	}

	return out
}

func MarkPrimaryTargetWithDefault(targets []NegotiationTarget, projectDir string) []NegotiationTarget {
	targets = MarkPrimaryTarget(targets, projectDir)
	if len(targets) == 0 {
		return []NegotiationTarget{{
			Name:       fallbackTargetName(projectDir), // 使用目录名
			ProjectDir: projectDir,
			Found:      true,
			Primary:    true,
			Reason:     "本轮执行",
		}}
	}

	return targets
}

// BuildTargetWarnings 面向用户展示非阻塞的告警
// 未命中 -> : 未找到材料
// 命中非主目标 ->: 材料找到,本轮不执行
func BuildTargetWarnings(targets []NegotiationTarget, projectDir string) []string {
	primaryDir := strings.TrimSpace(projectDir)

	warnings := make([]string, 0, len(targets))
	for _, target := range targets {
		name := strings.TrimSpace(target.Name)
		if name == pCommon.BlankString {
			continue
		}

		switch {
		case !target.Found:
			reason := strings.TrimSpace(target.Reason)
			if reason == pCommon.BlankString {
				reason = "未找到材料目录"
			}
			warnings = append(warnings, name+": "+reason+"（本轮跳过）")
		case primaryDir != pCommon.BlankString && strings.TrimSpace(target.ProjectDir) != projectDir:
			warnings = append(warnings, name+"：材料已找到，本轮不执行（可作为下一轮请求）")
		}
	}

	return warnings
}

// PromptNeedMoreInfoFeedback 存在可用目标时的"打回重试话术"
// 模型常见的错误:因为其中一个目标没找到,就整体need_more_info,会导致找到一个就该有计划的功能落空
func PromptNeedMoreInfoFeedback(usable NegotiationTarget) string {
	tempStr := "目标 " + usable.Name + " 已有材料目录 " + usable.ProjectDir + " ,不能因为其他目标缺失而整体 need_more_info。\n"
	tempStr += "请用 " + IntentionToolStart + " 且decision=proceed 重新提交：spec.project_dir 填 " + usable.ProjectDir + ",targets 里保留每个目标的探测结果。"

	return tempStr
}

// DemoteProceedFeedback 没有任何目标时提交给LLM的话术
func DemoteProceedFeedback() string {
	return "targets 里没有任何可用目标(全部 found=false)。\n" +
		"若确实都没有材料,请用 decision=need_more_info 重新提交,missing 里逐条列出追问项或候选绝对路径。"
}

func fallbackTargetName(dir string) string {
	name := filepath.Base(filepath.Clean(strings.TrimSpace(dir)))
	if name == pCommon.BlankString || name == "." || name == string(filepath.Separator) {
		return "未命名目标"
	}

	return name
}
