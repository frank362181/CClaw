package knowledge

import (
	"sync"
	"testing"
)

// TestTurnRecorderTwoPhase 锁住"工具留痕的两阶段契约"。
//
// 为什么需要两阶段:工具执行中被打断(用户点中断/超时)时,流式工具循环在
// "全部工具收尾之后的串行回填段"之前就退出了 —— 只记"结束"的写法会让这次调用
// 从历史与 t_turn_tool 里彻底消失,而它可能已经产生了副作用
// (见 interrupt-memory-chain.md M3)。
func TestTurnRecorderTwoPhase(t *testing.T) {
	r := NewTurnRecorder()

	idx := r.Start("npa_ingest", 1700000000)
	if idx != 0 {
		t.Fatalf("Start 应返回下标 0, got %d", idx)
	}

	// 只发起、未回填:必须仍在快照里,并被识别为"未完成"
	snap := r.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("只 Start 的记录应出现在快照, got %+v", snap)
	}
	if !snap[0].Unfinished() {
		t.Fatalf("未回填的记录应判定为未完成: %+v", snap[0])
	}
	if snap[0].ToolName != "npa_ingest" || snap[0].CallTime != 1700000000 {
		t.Fatalf("Start 记录的字段不对: %+v", snap[0])
	}

	// 回填后:已完成
	r.Finish(idx, 1700000005, "ok")
	snap = r.Snapshot()
	if snap[0].Unfinished() || snap[0].Result != "ok" || snap[0].RespTime != 1700000005 {
		t.Fatalf("Finish 未正确回填: %+v", snap[0])
	}
}

// TestTurnRecorderStartUnderConcurrency 覆盖同轮多工具并发:下标必须唯一且稳定
// (流式工具循环里同一轮的工具是并行执行的,回填靠这个下标定位)。
func TestTurnRecorderStartUnderConcurrency(t *testing.T) {
	r := NewTurnRecorder()
	const n = 8

	idxCh := make(chan int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			idxCh <- r.Start("tool", int64(i))
		}(i)
	}
	wg.Wait()
	close(idxCh)

	seen := make(map[int]bool, n)
	for idx := range idxCh {
		if idx < 0 || idx >= n {
			t.Fatalf("Start 返回非法下标: %d", idx)
		}
		if seen[idx] {
			t.Fatalf("Start 返回重复下标: %d", idx)
		}
		seen[idx] = true
	}
	if len(r.Snapshot()) != n {
		t.Fatalf("快照条数应为 %d, got %d", n, len(r.Snapshot()))
	}
}

// TestTurnRecorderNilAndInvalidIndex 覆盖 nil recorder(同步/流式共用同一份工具循环代码)
// 与非法下标:旁路记录不得 panic，也不得凭空产生记录。
func TestTurnRecorderNilAndInvalidIndex(t *testing.T) {
	var nilRec *TurnRecorder
	if idx := nilRec.Start("tool", 1); idx != -1 {
		t.Fatalf("nil recorder 的 Start 应返回 -1, got %d", idx)
	}
	nilRec.Finish(0, 1, "x")         // 不应 panic
	nilRec.Record("tool", 1, 2, "x") // 不应 panic
	if snap := nilRec.Snapshot(); snap != nil {
		t.Fatalf("nil recorder 的快照应为 nil, got %+v", snap)
	}

	r := NewTurnRecorder()
	r.Finish(-1, 1, "x") // 不应 panic
	r.Finish(9, 1, "x")  // 越界:不应 panic、也不应追加
	if len(r.Snapshot()) != 0 {
		t.Fatalf("非法下标不应产生记录: %+v", r.Snapshot())
	}

	// Record = Start + Finish 一步到位(脚本未注册等"不经执行阶段"的调用)
	r.Record("weather", 10, 12, "sunny")
	snap := r.Snapshot()
	if len(snap) != 1 || snap[0].ToolName != "weather" || snap[0].RespTime != 12 || snap[0].Unfinished() {
		t.Fatalf("Record 应写入一条已完成记录: %+v", snap)
	}
}

// TestTurnToolRecordUnfinished 锁住"未完成"的判定口径:RespTime<=0 即未返回。
func TestTurnToolRecordUnfinished(t *testing.T) {
	cases := []struct {
		name string
		rec  TurnToolRecord
		want bool
	}{
		{"未返回", TurnToolRecord{ToolName: "t", CallTime: 100}, true},
		{"零值", TurnToolRecord{}, true},
		{"已返回", TurnToolRecord{ToolName: "t", CallTime: 100, RespTime: 101}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.rec.Unfinished(); got != c.want {
				t.Fatalf("Unfinished() = %v, want %v", got, c.want)
			}
		})
	}
}
