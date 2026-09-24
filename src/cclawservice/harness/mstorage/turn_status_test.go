package mstorage

import (
	"testing"

	"my.favorite/cclawservice/model/dataobject"
)

// TestTurnStatusOf 锁住 t_turn.status 的判定优先级。
//
// 背景:resp_index=0 有三种来源(中断无产出 / 会话当时无历史文件 / 写回复失败),
// 单看 resp_index 无法区分;而 resp_index>0 也不能排除中断(中断但有产出)。
// 这里把"落库状态"的取舍固定下来:有助手记录优先,其次才是两种"没记录"的情形。
func TestTurnStatusOf(t *testing.T) {
	cases := []struct {
		name        string
		interrupted bool
		hasFile     bool
		respIndex   int
		appendFail  bool
		want        int
	}{
		{"正常完成:助手记录已落盘", false, true, 7, false, dataobject.TurnStatusNormal},
		{"中断且有部分产出:已落 assistant:interrupted", true, true, 9, false, dataobject.TurnStatusInterruptedPartial},
		{"中断且无产出:设计上不写助手记录", true, true, 0, false, dataobject.TurnStatusInterruptedEmpty},
		{"会话当时无历史文件(非中断)", false, false, 0, false, dataobject.TurnStatusRespNotWritten},
		{"会话当时无历史文件(中断)", true, false, 0, false, dataobject.TurnStatusRespNotWritten},
		{"助手记录写失败(非中断)", false, true, 0, true, dataobject.TurnStatusRespNotWritten},
		{"助手记录写失败(中断):写失败优先于中断语义", true, true, 0, true, dataobject.TurnStatusRespNotWritten},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := turnStatusOf(c.interrupted, c.hasFile, c.respIndex, c.appendFail)
			if got != c.want {
				t.Fatalf("turnStatusOf(%v, %v, %d, %v) = %d, want %d",
					c.interrupted, c.hasFile, c.respIndex, c.appendFail, got, c.want)
			}
		})
	}
}

// TestTurnStatusKeepsReasonInvariant 锁住不变量:status 为"中断族"(1/2)时,来源必须非零。
//
// 为什么必须锁:source 与 status 是两个维度,一旦某个分支漏传来源,按来源聚合就会漏统计;
// 而这两种状态恰恰是运维最关心的("这轮为什么没写完")。
func TestTurnStatusKeepsReasonInvariant(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		for _, hasFile := range []bool{false, true} {
			for _, respIndex := range []int{0, 5} {
				for _, appendFail := range []bool{false, true} {
					status := turnStatusOf(interrupted, hasFile, respIndex, appendFail)
					reason := interruptReasonOf(TurnOutcome{Interrupted: interrupted})

					if status == dataobject.TurnStatusInterruptedEmpty || status == dataobject.TurnStatusInterruptedPartial {
						if reason == dataobject.InterruptNone {
							t.Fatalf("status=%d 但来源为 0(interrupted=%v hasFile=%v respIndex=%d appendFail=%v)",
								status, interrupted, hasFile, respIndex, appendFail)
						}
					}
					if !interrupted && reason != dataobject.InterruptNone {
						t.Fatalf("未中断却给出了来源 %d", reason)
					}
				}
			}
		}
	}
}

// TestInterruptReasonOf 覆盖来源归一:未中断 → 0;残缺但漏传来源 → 兜底为"其它错误"。
func TestInterruptReasonOf(t *testing.T) {
	if got := interruptReasonOf(TurnOutcome{}); got != dataobject.InterruptNone {
		t.Fatalf("未中断的来源应为 InterruptNone, got %d", got)
	}

	if got := interruptReasonOf(TurnOutcome{Interrupted: true}); got != dataobject.InterruptOther {
		t.Fatalf("残缺但未给来源应兜底为 InterruptOther, got %d", got)
	}

	if got := interruptReasonOf(TurnOutcome{Interrupted: true, Reason: dataobject.InterruptByUser}); got != dataobject.InterruptByUser {
		t.Fatalf("应原样保留来源, got %d", got)
	}
}
