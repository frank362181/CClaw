/**
* 后台记忆维护的动作编排
*
* 单独成文件,是为了把"执行顺序"从协程闭包里提出来成为**可测的契约**:
* 顺序错了不会报错,只会静默地让蒸馏看不到已被压缩的原文(见 steps 的注释)。
**/

package mstorage

import (
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataobject"
)

// maintenanceStep 一个后台记忆维护动作。
type maintenanceStep int

const (
	stepDistill maintenanceStep = iota // 蒸馏长期记忆
	stepCompact                        // 压缩会话历史
	stepMerge                          // 合并摘要
)

// mergeAttemptState 一次合并尝试的结局。
type mergeAttemptState int

const (
	mergeWritten     mergeAttemptState = iota // 摘要已落盘(本轮取得进展)
	mergeNoCandidate                          // 挑不出可安全合并的集合
	mergeNoGain                               // 合并后不会变小
	mergeTooLarge                             // 输入超预算
	mergeLossy                                // 保全校验不达标(可退回更小集合重试)
	mergeEmptyOutput                          // 模型输出为空
)

func (s mergeAttemptState) String() string {
	switch s {
	case mergeWritten:
		return "written"
	case mergeNoCandidate:
		return "no-candidate"
	case mergeNoGain:
		return "no-gain"
	case mergeTooLarge:
		return "too-large"
	case mergeLossy:
		return "lossy"
	case mergeEmptyOutput:
		return "empty-output"
	}
	return "unknown"
}

// mergeAttemptSizes 合并的候选规模顺序:先"一次尽量多合并",失败再退回最小集合。
//
// 为什么要退一步(F6):保留率不达标时若直接放弃,而"摘要超预算"又禁用压缩,
// 系统就卡在"不再产生摘要 + 摘要也不变小"的状态 —— 历史继续被裁,却没有任何摘要代表它。
// 合并对象越少,关键标记越容易全部保住:这是**缩小一次处理的量**,不是降低保全校验的标准。
func mergeAttemptSizes(summaryCount int) []int {
	sizes := []int{0} // 0 = 不限制(最大前缀)
	if summaryCount > hCommon.CompactionMergeMinSummaries {
		sizes = append(sizes, hCommon.CompactionMergeMinSummaries)
	}
	return sizes
}

// MaintenanceRequest 一次后台记忆维护的输入(旁路编排的唯一入口参数)。
//
// 为什么是结构体而不是一串位置参数:
//   - TurnSeq(int) 与 Closing(bool) 相邻、Closing 又与摘要器相邻,位置传错编译期不报错;
//   - 具名字段让调用点自解释 —— 收尾路径(Assembled=nil + Closing=true)一眼可读。
//
// 字段**必须导出**:它出现在公开方法 TriggerMemoryMaintenance 的签名里,
// 包外无法构造的结构体等于把公开 API 变成摆设。
//
// 这里不持有 LLM 客户端:摘要器由调用方构造并注入(见 TriggerMemoryMaintenance 的 summarizer 参数),
// 本包不认识 LLM 客户端,只认识压好形的 hCommon.Summarizer。
type MaintenanceRequest struct {
	Session *dataobject.TSession //所属会话(ChatData 即历史文件名);必填
	Agent   *dataobject.TAgent   //所属 Agent(决定长期记忆文件位置与压缩模式);必填

	// Assembled 本轮上下文装配结果。收尾路径(EndChatSession)没有"本轮装配",传 nil ——
	// TriggerMemoryMaintenance 会把它归一为零值(预算未耗尽、无被裁区间)。
	Assembled *hCommon.AssembleResult
	// Records 装配时刚读回的记录快照,**只用于请求路径上的判定**(维护压力/压缩收益),0 额外 IO。
	// 后台动作读的是协程内的重读结果 —— 排队等锁期间可能已有其它轮次追加了记录或摘要。
	Records []hCommon.MemoryRecord

	// ContextWindow 模型上下文窗口(tokens),**只用于收窄维护调用的源文本预算**
	// (蒸馏/压缩/合并都要调模型,窗口装不下时会被整批跳过)。
	// 0 表示未知:此时沿用各自的硬上限,行为与未收口前完全一致。
	ContextWindow int

	TurnSeq int  //本轮落库的轮次序号(AppendTurn返回的; <=0 表示未落库,内部回退一次查询)
	Closing bool //会话收尾:必须带上尾巴
}

// ----------------------------- end of memory maintenance -------------------------

// maintenancePlan 本轮需要执行的维护动作集合。
//
// 构造只用**具名字段字面量**:maintenancePlan{distill: needDistill, compact: needCompact}。
// 三个紧邻的 bool 一旦做成位置参数,调换它们既不会编译报错、也不会改变类型,
// 只会静默地改变执行内容(蒸馏与压缩换了位 —— 正是下面 steps 注释里那个不可逆的错)。
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
