// functionCallingHeartbeat 工具调用期间的心跳
// 作用:工具脚本长时间无新输出时向UI推一行 progress,让UI能区分
//
//	"仍在运行" 与 "已经挂起" 的区别

package toolexecutor

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	cCommon "my.favorite/cclawservice/common"
	hCommon "my.favorite/cclawservice/harness/common"
	pCommon "my.favorite/platformutility/common"
)

type functionCallingHeartbeat struct {
	out     chan<- ToolEvent
	seq     int
	name    string
	startAt int64 //工具开始执行的时间(Unix 秒),用于计算运行了 N 秒

	lastOut atomic.Int64 //最后一次有新输出的时刻(UnixNano)

	stop chan struct{}
	done chan struct{}

	once sync.Once
}

func (h *functionCallingHeartbeat) Touch() {
	if h == nil {
		return
	}

	h.lastOut.Store(pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().NanoTimestamp)
}

// Emit 推送一行真实进度(调用方已经过滤 JSON)
func (h *functionCallingHeartbeat) Emit(line string) {
	if h == nil {
		return
	}

	h.trySend(ToolEvent{Phase: cCommon.ChatToolEventProgress, Seq: h.seq, Name: h.name, Detail: line})
}

func (h *functionCallingHeartbeat) Stop() {
	if h == nil {
		return
	}

	h.once.Do(func() { close(h.stop) })
	<-h.done
}

// ========================= private functions =========================

// trySend 非阻塞投递
func (h *functionCallingHeartbeat) trySend(e ToolEvent) bool {
	select {
	case h.out <- e:
		return true
	default:
		return false
	}
}

func startHeartbeat(ctx context.Context, out chan<- ToolEvent, seq int, name string, startAt int64) *functionCallingHeartbeat {
	interval := time.Duration(hCommon.ToolCallingHeartbeatInterval) * time.Second

	if interval <= 0 || out == nil {
		return nil
	}

	h := &functionCallingHeartbeat{
		out:     out,
		seq:     seq,
		name:    name,
		startAt: startAt,

		stop: make(chan struct{}),
		done: make(chan struct{}),
	}

	h.lastOut.Store(pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().NanoTimestamp)

	go func() {
		defer close(h.done)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				// 上游用户取消
				return
			case <-h.stop:
				return

			case now := <-ticker.C:
				if time.Duration(now.UnixNano()-h.lastOut.Load()) < interval {
					// 期间有新输出 -> 不打扰用户
					continue
				}
				h.lastOut.Store(now.UnixNano())

				if !h.trySend(ToolEvent{
					Phase:  cCommon.ChatToolEventProgress,
					Seq:    h.seq,
					Name:   h.name,
					Detail: fmt.Sprintf("...工具运行中(%d 秒无新输出)...", int(now.Unix()-h.startAt)),
				}) {
					return // 下游已关闭,没必要继续
				}
			}
		}
	}()

	return h
}
