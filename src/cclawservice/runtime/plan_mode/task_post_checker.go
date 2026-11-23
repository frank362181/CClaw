/**
* 目的：
*	计划中每步执行后的检查入口:输入只有"任务+产物+只读产物表"
*	而且仅仅看这一步执行后的结果是否准确
**/

package plan_mode

import (
	chatCommon "my.favorite/cclawservice/runtime/common"
	pCommon "my.favorite/platformutility/common"
)

// TaskBarrierInput 检查器的输入(仅含数据)
type TaskBarrierInput struct {
	Task   PlanTask
	Output Artifact
	Lookup func(name string) (Artifact, bool) // 只读产物表
	Spec   chatCommon.Spec
}

// Barrie 任务执行结果检查函数:返回空表示通过
type Barrie func(in TaskBarrierInput) []Gap

// BarrierRepository 检查器注册表(按SKILL.md的gate名索引)
type BarrierRepository struct {
	barriers map[string]Barrie
}

func NewBarrierRepository() *BarrierRepository {
	return &BarrierRepository{
		barriers: make(map[string]Barrie, 8),
	}
}

func (r *BarrierRepository) Registry(name string, g Barrie) {
	if name == pCommon.BlankString || g == nil {
		return
	}

	r.barriers[name] = g
}

func (r *BarrierRepository) Lookup(name string) (Barrie, bool) {
	gate, ok := r.barriers[name]
	return gate, ok
}

// Evaluate /** Evaluate 执行一个任务的事后检查
// 未注册的按fail处理:SOP声明了却没实现,属于配置错误
// 静默通过会让"没校验误当成 校验通过"
func (r *BarrierRepository) Evaluate(in TaskBarrierInput) []Gap {
	name := in.Task.Gate
	if name == pCommon.BlankString {
		return nil
	}

	gate, ok := r.Lookup(name)
	if !ok {
		return []Gap{{
			Code:     CodeTaskGateUnknown,
			TaskID:   in.Task.ID,
			Section:  in.Task.Section,
			Detail:   "unimplemented task gate" + name,
			Severity: SeverityFail,
		}}
	}

	return gate(in)
}
