/*
* 在转发上游增量的同时,为每个工具调用分配 **工具调用的序号**,并即时下发intent事件
*
* 设计原因:
*   一次用户提问会展开"多轮上游请求 * 每轮多个并发工具",事件来自三种并发上下文：
*		- intent/delta/thinking:网管 read loop(单协程)
*		- running/done/error:编排层(主协程)
*		- progress:工具执行协程 + 心跳协程
**/

package chat

import cCommon "my.favorite/cclawservice/common"

type seqSink struct {
	ISink // 转发其他事件;nil时由构造函数替换为 NopSink

	next int
	cur  map[int]int
}

func newSeqSink(s ISink) *seqSink {
	if s == nil {
		s = NopSink{}
	}

	return &seqSink{ISink: s, cur: make(map[int]int, 4)}
}

// SeqOf 返回key对应的序号;未分配则现场分配并缓存
// key < 0 表示"不参与缓存,每次都要新的" -- 用于生产者没有可靠index的场景
func (s *seqSink) SeqOf(key int) int {
	if key < 0 {
		return s.alloc()
	}

	if s, ok := s.cur[key]; ok {
		return s
	}

	seq := s.alloc()
	s.cur[key] = seq

	return seq
}

func (s *seqSink) OnDelta(delta string) {
	s.ISink.OnDelta(delta)
}

func (s *seqSink) OnToolIntent(index int, name string) {
	if index < 0 {
		index = 0 //病态上游:归一化,避免每次都"首次出现" -> 重复卡片
	}

	_, exists := s.cur[index]
	seq := s.SeqOf(index)
	if !exists {
		s.ISink.OnTool(ToolEvent{
			Phase: cCommon.ChatToolEventIntent,
			Seq:   seq,
			Name:  name,
		})
	}

	// 继续转发: 保持IStreamObserver契约完整(SSE 适配器里面)
	s.ISink.OnToolIntent(index, name)
}

// ======================= private functions ======================

// 开始新一轮:只清空本轮映射,**不动 next**
// 使用clear的目的是复用同一张表,避免每轮分配
func (s *seqSink) beginRound() { clear(s.cur) }

func (s *seqSink) alloc() int {
	s.next++
	return s.next
}

// 编译断言:seqSink必须满足完整的Sink
var _ ISink = (*seqSink)(nil)
