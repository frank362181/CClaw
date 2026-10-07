/*
* skill文档加载时的观察者
 */

package observatory

import "my.favorite/platformutility/logging"

// SkillSkipReason 一个被跳过的技能及其原因
type SkillSkipReason struct {
	SkillFile string
	Reason    string
}

// SkillLoadReport 一次 LoadSkills 的观测结果(命中/重载/跳过 + 原因)
type SkillLoadReport struct {
	Requested  int // 入参技能数
	Loaded     int // 返回的可用技能数
	Cached     int // 命中缓存(零读盘零解析)
	Reloaded   int // 首次加载或指纹变化:读盘 + YAML 解析
	Duplicated int // 重复绑定被去掉的条数

	// Skipped 被跳过的技能:分两批记录 —— 先记入参非法(目录为空/越界),
	// 再记读盘阶段失败(文件缺失/过大/YAML 处理失败)。每条带 SkillFile 可唯一定位。
	Skipped []SkillSkipReason
}

func NewSkillObserver(reqLen int) SkillLoadReport {
	return SkillLoadReport{
		Requested: reqLen,
	}
}

// Skip 记录一次跳过
func (r *SkillLoadReport) Skip(skillFile, reason string) {
	r.Skipped = append(r.Skipped, SkillSkipReason{SkillFile: skillFile, Reason: reason})
}

// LogSummary 按调用方给定的 scope(如 "chat service: agent=...,turn=...")打一行加载结果。
//   - 无跳过:debug 一行 —— 命中/重载计数就是“每轮零读盘”的线上证据;
//   - 有跳过:warn 并列出原因 —— 否则技能文件坏了只会表现为“模型忽然不再调该工具”。
func (r *SkillLoadReport) LogSummary(scope string) {
	if len(r.Skipped) == 0 {
		logging.LogDebugF("%s skills: requested=%d loaded=%d cached=%d reloaded=%d duplicated=%d",
			scope, r.Requested, r.Loaded, r.Cached, r.Reloaded, r.Duplicated)
		return
	}

	logging.LogWarnF("%s skills: requested=%d loaded=%d cached=%d reloaded=%d duplicated=%d skipped=%v",
		scope, r.Requested, r.Loaded, r.Cached, r.Reloaded, r.Duplicated, r.Skipped)
}
