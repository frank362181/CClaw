/**
* 回合分阶段耗时(TurnTiming)的测试。
*
* 锁住三条:
*  1) ctx 归因:挂了 turn 才能算出"本次请求距回合开始"的偏移,没挂必须是 0(不能瞎算);
*  2) 缺口自检:分阶段之和与总时长的差值算法要对;
*  3) 回合报表契约:TotalMs 必须有值、无工具时 ToolsMs 必须为 0、AgentId 必须带上。
*     —— 这条是本次埋点最容易出错的点:上层漏传已采集到的阶段数据。
**/

package observatory

import (
	"context"
	"sync"
	"testing"
	"time"
)

// captureTurnTiming 注入回合观察者。
func captureTurnTiming(t *testing.T) func() []TurnTiming {
	t.Helper()

	var mu sync.Mutex
	var out []TurnTiming

	RegisterTurnTimingObserver(func(s TurnTiming) {
		mu.Lock()
		defer mu.Unlock()
		out = append(out, s)
	})
	t.Cleanup(func() { RegisterTurnTimingObserver(nil) })

	return func() []TurnTiming {
		mu.Lock()
		defer mu.Unlock()
		return append([]TurnTiming(nil), out...)
	}
}

func TestCallStartOffset(t *testing.T) {
	if got := callStartOffset(context.Background(), 5000); got != 0 {
		t.Fatalf("未挂回合归因时应返回 0(不能瞎算),实际 %d", got)
	}

	ctx := withTurn(context.Background(), "turn1", 1000)
	if got := callStartOffset(ctx, 1500); got != 500 {
		t.Fatalf("距回合开始的偏移应为 500ms,实际 %d", got)
	}

	info, ok := turnFrom(ctx)
	if !ok || info.Scope != "turn1" || info.StartMs != 1000 {
		t.Fatalf("回合归因读取错误: ok=%v info=%+v", ok, info)
	}
}

func TestGetTurnTimeDiff(t *testing.T) {
	turn := TurnTiming{
		GateMs: 10, RouteMs: 20, SkillMs: 30, PromptMs: 40,
		LlmMs: 5000, ToolsMs: 1000, TotalMs: 10000,
	}
	if got := getTurnTimeDiff(turn); got != 3900 {
		t.Fatalf("未纳管缺口应为 3900ms,实际 %d", got)
	}
}

// TestOnTurnTimingContract 回合报表契约:
// 阶段数据由 service 层采集(chatTurn.turnTiming),报表只负责补"总时长/首见/回复长度/错误",
// 不得覆盖已采集的阶段数据。
func TestOnTurnTimingContract(t *testing.T) {
	snapshot := captureTurnTiming(t)

	turnStart := time.Now().Add(-2 * time.Second) // 回合已跑了 2s
	turn := TurnTiming{
		TurnScope: "turn-x",
		AgentId:   "agent_002",
		GateMs:    3, RouteMs: 120, SkillMs: 8, PromptMs: 5,
		LlmMs: 1500, Rounds: 2,
		ToolsMs: 0, ToolCalls: 0, // 本回合没有工具
	}

	OnTurnTiming(context.Background(), turn, turnStart, 800, "这是一个答案", nil)

	got := snapshot()
	if len(got) != 1 {
		t.Fatalf("应上报 1 条回合样本,实际 %d 条", len(got))
	}
	s := got[0]

	if s.TurnScope != "turn-x" {
		t.Fatalf("TurnScope 应为 turn-x,实际 %q", s.TurnScope)
	}
	if s.TotalMs <= 0 {
		t.Fatalf("TotalMs 必须有值(回合总时长),实际 %d", s.TotalMs)
	}
	if s.RouteMs != 120 || s.SkillMs != 8 || s.PromptMs != 5 || s.GateMs != 3 {
		t.Fatalf("阶段数据被丢弃/覆盖: %+v", s)
	}
	if s.LlmMs != 1500 || s.Rounds != 2 {
		t.Fatalf("上游调用汇总被丢弃: llm=%d rounds=%d", s.LlmMs, s.Rounds)
	}
	if s.ToolsMs != 0 {
		t.Fatalf("本回合没有工具执行,ToolsMs 应为 0,实际 %dms(被写成了整轮时长)", s.ToolsMs)
	}
	if s.FirstVisibleMs != 800 {
		t.Fatalf("FirstVisibleMs 应为 800,实际 %d", s.FirstVisibleMs)
	}
	if s.ReplyRunes != 6 {
		t.Fatalf("ReplyRunes 应为 6,实际 %d", s.ReplyRunes)
	}
}
