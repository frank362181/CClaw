/*
* skill文档加载时的观察者
 */

package observatory

import "my.favorite/platformutility/logging"

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
