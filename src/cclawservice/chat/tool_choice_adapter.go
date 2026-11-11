/**
* RoundPlannerAdapter:把 tool 的首轮强制策略适配成chat核心端口chat.IRoundPlanner
*
* 原因:
* - chat不能import harness/tool -- 否则 chat - tool - chat 形成cycle import
* - 端口之需要两个能力:这一轮发什么;与事后核对模型是否遵守
* - FirstRoundToolPlan:是 “声明层策略”(带Degrade/thinkingOff等诊断字段)
* - 核心层不需要知道这些细节,之需要RoundTools/Verify两个动作即可
*
* 首轮工具强制调用(force-call)策略:把 SKILL.md 里"哪个工具声明了 force-call"
* 变成一轮对话真正要发送的 tools + tool_choice。
*
* 背景(为什么要单独一个文件):
*   SKILL.md 的 force-call 是**声明在单个工具上**的(tools[i].force-call),
*   而工具调用循环的约束是**整轮生效**的 —— 两者粒度不同,必须有一处显式做转换,
*   否则只能像旧实现那样退化成一个 bool(任一工具声明就整轮 required),带来三个问题:
*
*   1) 过宽/归因错误:tool_choice="required" 的语义是"必须调用**某个**函数",不是"必须调用
*      get_weather"。A/B/C 三个工具的 schema 都发给模型时,模型完全可以调 A 或 C
*      来满足约束 —— 声明写在 B 上、强制却落在全体上,语义错位。
*      做法(2026-09-20 修正):**工具全量可见** + required。曾经试过"恰好 1 个强制
*      工具时首轮只发它",实测反而更糟 —— 首轮工具命名空间不完整会让模型判定
*      "其它工具未注册"(报告任务的首轮只看到 get_weather),而降级到 auto 后仍看不到。
*      详见 BuildFirstRoundToolPlan 内的注释。
*
*   2) 触发条件是工具集、不是本轮意图:只要 agent 挂了带 force-call 的技能,它这一轮
*      的**任何**问题(哪怕"你好")首轮都会带 required —— 会白烧一次工具调用,在
*      thinking 模型上还会直接 400(required 与自由推理的 thinking 解码互斥)。
*      本文件把"是否该强制"交给调用方(意图门),这里只负责"强制谁、怎么表达",
*      并把 ThinkingOff 一并返回,避免调用方漏掉"required 必须关 thinking"。
*
*   3) 失效无信号:若供应商/模板对 required 只做软约束而模型回了文本,调用方会把文本
*      当最终答复,强制调用**静默失效**。VerifyForcedCall 提供哨兵:发了 required 却
*      没等到 tool_calls 就告警。
*
* 职责边界:
*	- 纯函数,幂等
*	- Verify只做观测,不改变控制流
*	- 上游required 当软约束,不应该因此中断
**/

package chat

import (
	"fmt"
	"strings"

	"my.favorite/cclawservice/common/llmproto"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
)

// FunctionCallingThinking 描述大模型供应商能否执行强制调用的能力
type FunctionCallingThinking struct {
	ThinkingLocked bool // true时 -> 禁止下发 required/object形式的tool_choice
}

func GetLlmProviderThinking(llm dataobject.TLlm) FunctionCallingThinking {
	return FunctionCallingThinking{ThinkingLocked: llm.ThinkingLocked}
}

// 降级来源
const (
	DegradeNone       int = iota // 未降级
	DegradeConfig                // 配置缺口:forced 名与可用schema不匹配
	DegradeCapability            // 能力限制:供应商/模型做不到(如思考不可关闭)
)

// FirstRoundTool 一轮对话**首轮**应发送的工具集与 tool_choice。
//
// 只作用于首轮:第二轮起调用方必须换回全量工具 + 不带 tool_choice
// (用 RoundTools(all, round) 即可,别手写分支)。否则模型调完强制工具后就再也
// "看不见"其余工具,多步流程会卡死。
type FirstRoundTool struct {
	Tools         []llmproto.Tool // 首轮实际发送的工具集(降级时为全量)
	ToolChoice    string          // 首轮 tool_choice;空串 = 不发该字段(等价 auto)
	ForcedTools   []string        // 生效的强制工具(与可用工具求交集后的结果)
	Degraded      bool            // true = 强制调用被降级为 auto(见 DegradeReason)
	DegradeKind   int             // 降级来源
	DegradeReason string          // 降级原因(便于日志一眼看懂,不靠推测)
}

// BuildFirstRoundTool 依据"声明层强制工具名"生成本轮首轮策略。
//   - all:         本 Agent 本轮全部可用工具(已解析成功的 schema,顺序即声明顺序)
//   - forcedNames: CollectForcedToolNames 的结果(声明了 force-call 的工具名)
//
// 决策(按"生效强制工具个数"分支):
//
//	0 个 → 全量 tools + 不发 tool_choice(auto)。
//	      含两种情形:没有工具声明 force-call;或声明了但没有一个在可用工具集里(见下"配置降级")。
//	≥1 个 → 全量 tools + required。协议层一轮只能强制一个**具体**函数,
//	      "这几个都调"表达不了,只能保证"至少调一个",由模型自选。
//
// **工具集恒为全量可见,任何分支都不裁剪**:首轮若只发被强制的那一个,
// 会让模型判定"其它工具未注册";降级为 auto 后它也看不到其它工具,任务直接做不了。
//
// 三处必须的兜底(优先级从高到低,顺序不可交换):
//  1. all 为空 → 不发 tool_choice(tools=[] 配 required 是非法请求,上游 400);
//  2. 强制名与可用 schema 无交集 → 降级 auto。这是**配置缺口**(脚本缺失/schema 改名/
//     技能禁用后残留声明),必须先于能力裁决报出,且必须有人修;
//  3. 供应商思考不可关闭 → 降级 auto。这是**能力限制**,属长期预期。
//
// 为什么必须在下发前就判掉第 3 种:
// tool_choice=required 要求解码落在 tool-call 语法内,与 thinking 的自由推理互斥。
// 关 thinking 的唯一落点是 chat/request_builder.go,但它救不了"固有思考"的模型
// (enable_thinking=false 被忽略),只能在**建计划时**就不发 required;
// 否则每轮工具调用都要白付一次 400 + Degrade 重试(实测 ~500ms + 一条 error 指标)。
func BuildFirstRoundTool(all []llmproto.Tool, forcedNames []string, callingThinking FunctionCallingThinking) FirstRoundTool {
	firstTool := FirstRoundTool{Tools: all}

	// 无工具可用 -> 绝不发 tool_choice
	if len(all) == 0 {
		firstTool.Degraded, firstTool.DegradeKind = true, DegradeConfig
		firstTool.DegradeReason = "no available tools"
		return firstTool
	}

	forced := pCommon.NormalizeStrings(forcedNames)
	if len(forced) == 0 {
		return firstTool // 没有强制调用:全量工具 + auto
	}

	functionNames := toolNames(all)
	effective := filterOverlapToolNames(forced, functionNames)
	if len(effective) == 0 {
		// 声明了强制工具,但一个都不在可用集合里 —— 绝不发 required:
		// 白名单为空 + required 会给上游一个自相矛盾的请求。
		firstTool.Degraded, firstTool.DegradeKind = true, DegradeConfig
		firstTool.DegradeReason = fmt.Sprintf("forced tools %v not present in available tools %v", forced, functionNames)
		return firstTool
	}

	firstTool.ForcedTools = effective

	if callingThinking.ThinkingLocked {
		firstTool.Degraded, firstTool.DegradeKind = true, DegradeCapability
		firstTool.DegradeReason = fmt.Sprintf(
			"provider thinking is locked (cannot be disabled), tool_choice=required would be rejected upstream; degrade to auto, forced=%v",
			effective)
		return firstTool
	}

	firstTool.ToolChoice = llmproto.ToolChoiceRequired
	return firstTool
}

type RoundToolAdapter struct {
	plan  FirstRoundTool
	scope string // "agent=<id>,turn=<id>:日志反复用
}

func NewRoundToolAdapter(plan FirstRoundTool, scope string) RoundToolAdapter {
	a := RoundToolAdapter{
		plan:  plan,
		scope: strings.TrimSpace(scope),
	}

	if plan.Degraded {
		switch plan.DegradeKind {
		case DegradeConfig:
			logging.LogWarnF("tool plan degraded: scope=%s reason=%s tools=%v forced=%v,degrade kind=%d",
				a.scope, plan.DegradeReason, toolNames(plan.Tools), plan.ForcedTools, plan.DegradeKind)
		case DegradeCapability:
			logging.LogWarnF("tool plan degraded: scope=%s reason=%s tools=%v forced=%v,degrade kind=%d",
				a.scope, plan.DegradeReason, toolNames(plan.Tools), plan.ForcedTools, plan.DegradeKind)
		default:
			logging.LogWarnF("tool plan degraded: scope=%s reason=%s tools=%v forced=%v",
				a.scope, plan.DegradeReason, toolNames(plan.Tools), plan.ForcedTools)
		}
	}

	return a
}

// RoundTools 返回实际下发的工具与tool_choice
// 规则收敛在 FirstRoundTool。RoundTools内
// - round== 0: 计划里的工具 + 计划里的tool_choice(可能是required)
// - round>0:全量工具 + 空串(等价auto) -- 否则模型调完工具有就再也看不见其他工具,多步会卡死
func (rpAdapter RoundToolAdapter) RoundTools(all []llmproto.Tool, round int) (tools []llmproto.Tool, toolChoice string) {
	if len(all) == 0 {
		return nil, pCommon.BlankString
	}

	if round <= 0 {
		return rpAdapter.plan.Tools, rpAdapter.plan.ToolChoice
	}

	return all, pCommon.BlankString
}

// Verify 本轮声明的强制策略是否被模型遵守
// 仅做观测,不返回错误、不改变控制流。三类不一致分别对应三条日志:
// - 1.声明了required,却一条tool_call都没返回(供应商将required当作软约束)
// - 2.有tool_call,但没一个被强制的(协议合法,业务预期落空)
// - 3.有tool_call,但名字不存在本轮下发的schema里(模型幻觉 / 工具集体认知不一致)
func (rpAdapter RoundToolAdapter) Verify(calls []llmproto.ToolCall, toolChoice string) {
	// 只有真正下发了 required,才存在"是否遵守这个问题"
	if toolChoice != llmproto.ToolChoiceRequired {
		return
	}

	calledTools := calledToolNames(calls)

	// 1. 完全没有tool_call
	if len(calledTools) == 0 {
		logging.LogWarnF("tool choice: forced call requested but no tool_call returned, scope=%s forced=%v tools=%v "+
			"(provider may treat tool_choice as a soft constraint; next round falls back to auto)",
			rpAdapter.scope, rpAdapter.plan.ForcedTools, toolNames(rpAdapter.plan.Tools))
		return
	}

	// ② 调了,但没调被强制的那个
	if len(rpAdapter.plan.ForcedTools) > 0 && !pCommon.Intersects(calledTools, rpAdapter.plan.ForcedTools) {
		logging.LogWarnF("tool choice: forced tools were not used although tool_choice=required, scope=%s forced=%v called=%v",
			rpAdapter.scope, rpAdapter.plan.ForcedTools, calledTools)
	}

	// ③ 调了本轮 schema 里没有的工具
	if unknown := pCommon.Unknowns(calledTools, toolNames(rpAdapter.plan.Tools)); len(unknown) > 0 {
		logging.LogWarnF("tool choice: model called tools absent from this round's schema, scope=%s unknown=%v tools=%v",
			rpAdapter.scope, unknown, toolNames(rpAdapter.plan.Tools))
	}
}

// NoToolsAdapter 永不发工具的合法计划
// 存在的意义:让TurnState.Plan永远非 nil 成为不变量
type NoToolsAdapter struct{}

func (NoToolsAdapter) RoundTools([]llmproto.Tool, int) ([]llmproto.Tool, string) {
	return nil, pCommon.BlankString
}

func (NoToolsAdapter) Verify([]llmproto.ToolCall, string) {}

// 取出一组工具声明的名字(保序)。
func toolNames(tools []llmproto.Tool) []string {
	if len(tools) == 0 {
		return nil
	}

	out := make([]string, 0, len(tools))
	for i := range tools {
		name := strings.TrimSpace(tools[i].Function.Name)
		if name == pCommon.BlankString {
			continue
		}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func calledToolNames(calls []llmproto.ToolCall) []string {
	if len(calls) == 0 {
		return nil
	}

	out := make([]string, 0, len(calls))
	for i := range calls {
		if name := strings.TrimSpace(calls[i].Function.Name); name != pCommon.BlankString {
			out = append(out, name)
		}
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

// filterOverlapToolNames 求"想强制的名字"与"可用名字"的交集,顺序以 want 为准。
func filterOverlapToolNames(want []string, available []string) []string {
	if len(want) == 0 || len(available) == 0 {
		return nil
	}

	has := make(map[string]struct{}, len(available))
	for _, name := range available {
		has[name] = struct{}{}
	}

	out := make([]string, 0, len(want))
	for _, name := range pCommon.NormalizeStrings(want) {
		if _, ok := has[name]; ok {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
