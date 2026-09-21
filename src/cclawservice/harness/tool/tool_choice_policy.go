/**
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
**/

package tool

import (
	"fmt"
	"strings"

	"my.favorite/cclawservice/llmproto"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
)

// ==================== 声明层聚合 ====================

// ForcedCallDecl 声明层工具的最小投影:只看"名字 + 是否要求强制调用"。
//
// 为什么不直接用 parsedTool / BasicTool:这两种类型分属不同包且在演进中
// (harness 内部的 parsedTool、harness/tool 的 BasicTool),让策略层依赖具体声明类型
// 会把工具声明模型的重构传导到这里。调用方用一层 3 行投影即可,本文件保持零耦合。
type ForcedCallDecl struct {
	Name      string // LLM 侧的 function name(即 SKILL.md tools[i].name,已 TrimSpace)
	ForceCall bool   // SKILL.md tools[i].force-call
}

// CollectForcedToolNames 聚合出"本轮需要强制调用"的工具名(去重、保序)。
//
// 名称必须与"发给模型的 schema 名"和"模型回传的 tool_calls[].function.name"完全同源,
// 否则白名单过滤与执行查表都会失配:
//   - ✅ get_weather        (tools[i].name)
//   - ❌ weather            (技能名 / skill_dir)
//   - ❌ scripts/weather.py (脚本相对路径)
//
// 空值/纯空白会被丢弃(声明写了 name 才可能进 schema,见各解析器的空名跳过逻辑)。
func CollectForcedToolNames(decls []ForcedCallDecl) []string {
	names := make([]string, 0, len(decls))
	for _, d := range decls {
		if !d.ForceCall {
			continue
		}
		names = append(names, d.Name)
	}
	return pCommon.NormalizeStrings(names)
}

// FunctionNames 取出一组工具声明的名字(保序)。
func FunctionNames(tools []llmproto.Tool) []string {
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

// FilterToolsByName 按 names 过滤 tools,输出顺序**以 tools 的声明顺序为准**。
//
// 用声明顺序而不是 names 顺序:请求体稳定(同一份 SKILL.md 每次产出同一串 JSON),
// 对上游 prompt 缓存、日志比对、以及"同一轮内容可复现"都更友好。
func FilterToolsByName(tools []llmproto.Tool, names []string) []llmproto.Tool {
	if len(tools) == 0 {
		return nil
	}
	normalized := pCommon.NormalizeStrings(names)
	if len(normalized) == 0 {
		return nil
	}

	want := make(map[string]struct{}, len(normalized))
	for _, name := range normalized {
		want[name] = struct{}{}
	}

	out := make([]llmproto.Tool, 0, len(normalized))
	for i := range tools {
		if _, ok := want[strings.TrimSpace(tools[i].Function.Name)]; ok {
			out = append(out, tools[i])
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ==================== 首轮计划 ====================

// FirstRoundToolPlan 一轮对话**首轮**应发送的工具集与 tool_choice。
//
// 只作用于首轮:第二轮起调用方必须换回全量工具 + 不带 tool_choice
// (用 RoundTools(all, round) 即可,别手写分支)。否则模型调完强制工具后就再也
// "看不见"其余工具,多步流程会卡死。
type FirstRoundToolPlan struct {
	Tools         []llmproto.Tool // 首轮实际发送的工具集(降级时为全量)
	ToolChoice    string          // 首轮 tool_choice;空串 = 不发该字段(等价 auto)
	ForcedTools   []string        // 生效的强制工具(与可用工具求交集后的结果)
	ThinkingOff   bool            // true = 本次请求必须显式关闭 thinking(流式也要)
	Degraded      bool            // true = 强制调用被降级为 auto(见 DegradeReason)
	DegradeReason string          // 降级原因(便于日志一眼看懂,不靠推测)
}

// RequiresForcedCall 报告首轮是否真的下发了强制调用。
func (p FirstRoundToolPlan) RequiresForcedCall() bool {
	return p.ToolChoice == llmproto.ToolChoiceRequired
}

// RoundTools 返回第 round 轮(从 0 计)应发送的工具集与 tool_choice。
//
// "只裁首轮"这条规则收敛在这里,调用方即使写成循环也不会用错:
// 第 0 轮用计划里的白名单,之后一律回全量 + auto。
func (p FirstRoundToolPlan) RoundTools(all []llmproto.Tool, round int) ([]llmproto.Tool, string) {
	if round <= 0 {
		return p.Tools, p.ToolChoice
	}
	return all, pCommon.BlankString
}

// String 便于日志/测试断言一眼核对计划内容。
func (p FirstRoundToolPlan) String() string {
	choice := p.ToolChoice
	if choice == pCommon.BlankString {
		choice = llmproto.ToolChoiceAuto
	}
	desc := fmt.Sprintf("tools=%v choice=%s forced=%v thinking_off=%t",
		FunctionNames(p.Tools), choice, p.ForcedTools, p.ThinkingOff)
	if p.Degraded {
		desc += " degraded=" + p.DegradeReason
	}
	return desc
}

// BuildFirstRoundToolPlan 依据"声明层强制工具名"生成本轮首轮策略。
//   - all:         本 Agent 本轮全部可用工具(已解析成功的 schema,顺序即声明顺序)
//   - forcedNames: CollectForcedToolNames 的结果(声明了 force-call 的工具名)
//
// 决策(按"生效强制工具个数"分支):
//
//	0 个 → 全量 tools + 不发 tool_choice(auto)。含两种情形:
//	      没有工具声明 force-call;或声明了但**没有一个在可用工具集里**(降级,见下)。
//	1 个 → 只发该工具 + required。这是"必须调用它"的最强表达:
//	      掩码只保留 tool-call 语法、函数名唯一,模型没有第二个可选项。
//	≥2 个 → 全量 tools + required。协议层一轮只能强制一个**具体**函数,
//	      要"这几个都调"表达不了,只能保证"至少调一个",由模型自选;
//	      真要"都调"必须靠分轮或提示词约束。
//
// 两个必须的兜底:
//   - all 为空 → 不发 tool_choice。tools=[] 配 required 是非法请求,上游会 400;
//   - 声明了强制工具但都不在 all 里(脚本缺失被跳过、声明名与 schema 名不一致、
//     技能被禁用后残留声明)→ 降级为 auto,并把原因写进 DegradeReason。
//     这里最容易出的事故是"静默降级":白名单过滤成空数组还照发 required。
//
// ThinkingOff:tool_choice=required 与 thinking 模式互斥(thinking 阶段要自由生成
// 推理文本,required 要求解码只落在 tool-call 语法内),供应商会直接拒绝
// (DashScope 原文:"The tool_choice parameter does not support being set to
// required or object in thinking mode")。所以凡是下发 required 的首轮,调用方
// **流式也要**显式 enable_thinking=false —— 这是唯一被验证可行(200 + 正常 tool_calls)
// 的组合。
func BuildFirstRoundToolPlan(all []llmproto.Tool, forcedNames []string) FirstRoundToolPlan {
	plan := FirstRoundToolPlan{Tools: all}

	if len(all) == 0 {
		plan.Degraded = true
		plan.DegradeReason = "no available tools"
		return plan
	}

	forced := pCommon.NormalizeStrings(forcedNames)
	if len(forced) == 0 {
		return plan // 没有强制调用:全量工具 + auto
	}

	effective := FilterOverlapToolNames(forced, FunctionNames(all))
	if len(effective) == 0 {
		// 声明了强制工具,但一个都不在可用集合里 —— 绝不发 required:
		// 白名单为空 + required 会给上游一个自相矛盾的请求。
		plan.Degraded = true
		plan.DegradeReason = fmt.Sprintf("forced tools %v not present in available tools %v", forced, FunctionNames(all))
		return plan
	}

	plan.ForcedTools = effective
	plan.ToolChoice = llmproto.ToolChoiceRequired
	plan.ThinkingOff = true

	// 刻意**不再**把工具列表裁成“只有被强制的那一个”。
	//
	// 曾经的写法是“恰好 1 个强制工具 → 首轮只发它”(为了精确到具体函数),
	// 2026-09-20 实测证明这个收益不成立、代价很大:
	//   - 两家供应商的 thinking 模型都拒 required/object(DashScope 需关 thinking 才接受,
	//     DeepSeek 的思考是模型固有的、enable_thinking 无效)⇒ “精确强制”本来就不稳;
	//   - 而首轮工具命名空间不完整会直接误导模型:报告任务的首轮只看到 get_weather,
	//     模型据此宣称“npa_* 本轮未注册/可见”(AGENTS.md 说存在、schema 里没有);
	//   - 降级为 auto 重试时工具列表也不变 ⇒ 模型仍然看不到其它工具,任务直接做不了;
	//   - 首轮若 len(calls)==0 就当作最终答复返回,后面的轮次(本可恢复全量)根本不会发生。
	//
	// 结论:工具**全量可见**(最坏情况是模型调错一个工具,但它知道有哪些工具可用),
	// 由 required 保证“至少调用一个”。精确到具体函数如果需要,应走 tool_choice 对象形式,
	// 而那需要先改 llmproto.ToolChoice 的类型(目前是 string)。
	plan.Tools = all
	return plan
}

// FilterOverlapToolNames 求"想强制的名字"与"可用名字"的交集,顺序以 want 为准。
func FilterOverlapToolNames(want []string, available []string) []string {
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

// ==================== 强制生效哨兵 ====================

// VerifyForcedCall 校验"下发了 required,模型是否真的给了工具调用",返回是否达标。
// scope 用于定位来源(如 "agent=agent_002 turn=xxx"),便于从日志反查。
func VerifyForcedCall(plan FirstRoundToolPlan, calls []llmproto.ToolCall, roundToolChoice, scope string) bool {
	if roundToolChoice != llmproto.ToolChoiceRequired {
		return true // 本轮没要求强制,不校验(否则会误报)
	}
	if len(calls) > 0 {
		return true
	}

	logging.LogWarnF("tool choice: forced call was requested but no tool_call returned, scope=%s forced=%v tools=%v "+
		"(provider may apply tool_choice as a soft constraint; consider retry or downgrade to auto)",
		scope, plan.ForcedTools, FunctionNames(plan.Tools))
	return false
}
