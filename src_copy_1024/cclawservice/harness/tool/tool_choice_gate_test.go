package tool

import (
	"testing"

	"my.favorite/cclawservice/harness/api/llmproto"
)

// TestIntentGateAndToolNamespace 把 2026-09-20 的两次事故固化成断言:
//
//  1. 报告任务被“天气技能的 force-call”逼着调天气工具(agent 级强制的副作用);
//  2. 首轮工具列表被裁成只剩 get_weather,模型据此判定“其它工具未注册”,任务失能。
//
// 同时锁住首轮/后续轮必须“第二轮回到全量 + auto”,以及哨兵只在“本轮真的发了 required”时校验。
func TestIntentGateAndToolNamespace(t *testing.T) {
	bundle := ToolsBundle{
		Tools: []llmproto.Tool{
			{Type: "function", Function: llmproto.ToolFunction{Name: "get_weather"}},
			{Type: "function", Function: llmproto.ToolFunction{Name: "npa_ingest"}},
			{Type: "function", Function: llmproto.ToolFunction{Name: "npa_save_report"}},
		},
		Slots: []SkillSlot{
			{SkillId: "weather", File: "/s/weather/SKILL.md", Keywords: []string{"天气", "气温"}, ForcedTools: []string{"get_weather"}},
			{SkillId: "npa-report", File: "/s/npa-report/SKILL.md", Keywords: []string{"报告", "不良"}},
		},
	}

	// ---- 报告任务:不许强制天气,工具列表必须全量 ----
	act := bundle.Activate("请读取档案目录下的所有文件，分析个贷不良资产并形成报告")
	if len(act.ForcedTools) != 0 {
		t.Fatalf("报告任务不应强制任何工具, got %v", act.ForcedTools)
	}
	if len(act.ActiveSkills) != 1 || act.ActiveSkills[0] != "npa-report" {
		t.Fatalf("应只命中 npa-report, got %v", act.ActiveSkills)
	}
	if len(act.SuppressedForce) != 1 || act.SuppressedForce[0] != "get_weather" {
		t.Fatalf("应记录被抑制的强制工具, got %v", act.SuppressedForce)
	}

	plan := BuildFirstRoundToolPlan(bundle.Tools, act.ForcedTools)
	if plan.RequiresForcedCall() {
		t.Fatal("报告任务不应下发 required")
	}
	tools, choice := plan.RoundTools(bundle.Tools, 0)
	if len(tools) != len(bundle.Tools) || choice != "" {
		t.Fatalf("首轮应全量工具 + 不发 tool_choice, got tools=%d choice=%q", len(tools), choice)
	}

	// ---- 天气问题:命中并强制,但工具列表仍然全量(不再窄化) ----
	act2 := bundle.Activate("查询今天北京的天气")
	plan2 := BuildFirstRoundToolPlan(bundle.Tools, act2.ForcedTools)
	if !plan2.RequiresForcedCall() || !plan2.ThinkingOff {
		t.Fatalf("天气问题应强制调用并关闭 thinking, got choice=%q thinking_off=%v", plan2.ToolChoice, plan2.ThinkingOff)
	}
	tools2, choice2 := plan2.RoundTools(bundle.Tools, 0)
	if len(tools2) != len(bundle.Tools) {
		t.Fatalf("首轮不得窄化工具列表: got %d, want %d", len(tools2), len(bundle.Tools))
	}
	if choice2 != llmproto.ToolChoiceRequired {
		t.Fatalf("首轮 choice 应为 required, got %q", choice2)
	}

	// ---- 第二轮:回到全量 + auto ----
	tools3, choice3 := plan2.RoundTools(bundle.Tools, 1)
	if len(tools3) != len(bundle.Tools) || choice3 != "" {
		t.Fatalf("第二轮应全量 + auto, got tools=%d choice=%q", len(tools3), choice3)
	}

	// ---- 哨兵:按本轮实际发送的 choice 判断(空 choice 不得误报) ----
	if !VerifyForcedCall(plan2, nil, "", "test") {
		t.Fatal("本轮没发 required 时哨兵不得告警")
	}
	if VerifyForcedCall(plan2, nil, llmproto.ToolChoiceRequired, "test") {
		t.Fatal("发了 required 却没有 tool_call 时应返回 false")
	}
	if !VerifyForcedCall(plan2, []llmproto.ToolCall{{Function: llmproto.ToolCallFunction{Name: "get_weather"}}}, llmproto.ToolChoiceRequired, "test") {
		t.Fatal("有 tool_call 时应达标")
	}

	// ---- 未声明触发词的技能:保持旧行为(允许强制) ----
	legacy := ToolsBundle{
		Tools: bundle.Tools,
		Slots: []SkillSlot{{SkillId: "legacy", File: "/s/legacy/SKILL.md", ForcedTools: []string{"get_weather"}}},
	}
	if act3 := legacy.Activate("随便问点什么"); len(act3.ForcedTools) != 1 || len(act3.Legacy) != 1 {
		t.Fatalf("未声明关键词的技能应保持旧行为(允许强制), got forced=%v legacy=%v", act3.ForcedTools, act3.Legacy)
	}
}
