/**
* plan mode场景执行计划所需的参数和结构体定义
**/

package plan_mode

import (
	"time"

	cCommon "my.favorite/cclawservice/common"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/toolexecutor"
	"my.favorite/cclawservice/model/dataobject"
	chatCommon "my.favorite/cclawservice/runtime/common"
)

// NegotiationInput 协商的3，4道闸门的输入参数(协商 + spec校验 + 计划展开)
type NegotiationInput struct {
	SessionID string
	TurnID    string
	Message   string
	Provider  dataobject.TLlm // LLM provider
	WorkRoot  string

	SkillID  string
	SkillDoc hCommon.SkillDocument

	NegSpec chatCommon.INegotiationSpec // 由service层注入

	Sink  cCommon.ISink
	Rules toolexecutor.ToolConfigRules
}

// NegotiationOutput 三态语义:
//
//	Ready = true -> 参数可用,Spec 有效,可以继续展开计划
//	End = true -> 本轮已由协商给出结论(追问 / 参数不可用)
//	两者都是false -> 未进入 / 不适用 / 协商失败,调用方落回单轮直聊
type NegotiationOutput struct {
	Ready bool
	Spec  chatCommon.Spec

	End    bool   //表示协商是否终结
	Answer string // End = true 时作为本轮正文
}

// PlanModeEngineInput 计划执行的输入
// 并发闸、中断登记、超时的前置都在 service完成，这里只是接受参数
type PlanModeEngineInput struct {
	AgentID   string
	TurnID    string
	SessionID string

	Plan Plan
	Spec chatCommon.Spec

	Sink cCommon.ISink
	Tool ISopToolInvoker // 由service层的NewSOPTurnToolInvoker创建注入

	StartAt time.Time
}

// PlanModeEngineOutput 计划执行的结果;转换成Reply由service层来完成
type PlanModeEngineOutput struct {
	Answer string // 面向用户的最终答复 (GetSOPChatResultSummary的产物）
	State  *PlanState
	Gaps   []Gap
	Waves  int
}
