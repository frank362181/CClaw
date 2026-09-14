/**
* 后台记忆维护的动作编排
*
* 单独成文件,是为了把"执行顺序"从协程闭包里提出来成为**可测的契约**:
* 顺序错了不会报错,只会静默地让蒸馏看不到已被压缩的原文(见 steps 的注释)。
**/

package service

// maintenanceStep 一个后台记忆维护动作。
type maintenanceStep int

const (
	stepDistill maintenanceStep = iota // 蒸馏长期记忆
	stepCompact                        // 压缩会话历史
	stepMerge                          // 合并摘要
)

// maintenancePlan 本轮需要执行的维护动作集合。
type maintenancePlan struct {
	distill bool // 蒸馏长期记忆(轮次节拍触发)
	compact bool // 压缩会话历史(历史被预算裁掉时触发)
	merge   bool // 合并摘要(摘要总量超预算时触发)
}

// steps 按**执行顺序**返回动作序列。
//
// 顺序是契约,不是实现细节:
//   - **蒸馏必须在压缩之前**。蒸馏的输入是"未被摘要代表的普通记录"(hCommon.ActivePlainRecords),
//     而压缩会把整段记录变成一条摘要记录;若先压缩,那段原文对蒸馏**永久不可见**
//     (摘要记录是压缩产物,不参与蒸馏),长期记忆就再也拿不到早期对话里的事实。
//   - 合并放在最后:它依据"压缩之后"的摘要集合与总量判断(压缩本身可能把总量推过阈值)。
func (p maintenancePlan) steps() []maintenanceStep {
	steps := make([]maintenanceStep, 0, 3)
	if p.distill {
		steps = append(steps, stepDistill)
	}
	if p.compact {
		steps = append(steps, stepCompact)
	}
	if p.merge {
		steps = append(steps, stepMerge)
	}
	return steps
}
