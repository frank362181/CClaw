/**
* TurnRunner 的"执行路径选择"回归测试。
*
* 为什么必须单测这一层:
*   工具路径按"是否流式"选择不同的 looper —— 流式走并行执行器(带进度事件),
*   非流式走串行执行器(无事件通道)。选错**不会报错**,只表现为并发度、副作用
*   顺序与 dispatch 失败语义的改变(静默行为漂移),冒烟与端到端都测不出来。
*
* 本测试只覆盖"选哪个",不覆盖 looper 内部行为。
**/

package chat

import (
	"context"
	"testing"

	"my.favorite/cclawservice/common/llmproto"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/toolexecutor"
	"my.favorite/cclawservice/model/dataobject"
)

// fakeToolLooper 记录自己被调用了几次。
type fakeToolLooper struct {
	calls int
}

func (f *fakeToolLooper) Run(_ context.Context, _ *TurnState, _ ISink, _ toolexecutor.ToolConfigRules) (llmproto.LlmReply, error) {
	f.calls++

	return llmproto.LlmReply{}, nil
}

// TestTurnRunnerInvoke_PicksLooperByStream 锁定 invoke 的分流规则:
//
//	有工具 + 流式   → asyncLooper
//	有工具 + 非流式 → syncLooper
func TestTurnRunnerInvoke_PicksLooperByStream(t *testing.T) {
	cases := []struct {
		name      string
		streamed  bool
		wantSync  int
		wantAsync int
	}{
		{name: "非流式走串行执行器", streamed: false, wantSync: 1, wantAsync: 0},
		{name: "流式走并行执行器", streamed: true, wantSync: 0, wantAsync: 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			syncLooper := &fakeToolLooper{}
			asyncLooper := &fakeToolLooper{}

			r := &TurnRunner{
				syncLooper:  syncLooper,
				asyncLooper: asyncLooper,
			}

			turn := &TurnState{VisibleTools: []llmproto.Tool{{}}}
			turn.SetStreamed(c.streamed)

			if _, err := r.invoke(context.Background(), turn, NopSink{}); err != nil {
				t.Fatalf("invoke 失败: %v", err)
			}

			if syncLooper.calls != c.wantSync || asyncLooper.calls != c.wantAsync {
				t.Fatalf("looper 选择错误: sync=%d async=%d, want sync=%d async=%d",
					syncLooper.calls, asyncLooper.calls, c.wantSync, c.wantAsync)
			}
		})
	}
}

// TestTurnRunnerInvoke_NoToolsSkipsLoopers 无工具时必须一次调用,
// 不得因 VisibleTools 为空就误入工具循环。
func TestTurnRunnerInvoke_NoToolsSkipsLoopers(t *testing.T) {
	syncLooper := &fakeToolLooper{}
	asyncLooper := &fakeToolLooper{}

	r := &TurnRunner{
		syncLooper:  syncLooper,
		asyncLooper: asyncLooper,
		assembler:   stubAssembler{},
		transport:   &stubTransport{},
	}

	turn := &TurnState{} // VisibleTools 为空
	turn.SetStreamed(false)

	if _, err := r.invoke(context.Background(), turn, NopSink{}); err != nil {
		t.Fatalf("invoke 失败: %v", err)
	}

	if syncLooper.calls != 0 || asyncLooper.calls != 0 {
		t.Fatalf("无工具时不应进入工具循环: sync=%d async=%d", syncLooper.calls, asyncLooper.calls)
	}
}

// stubAssembler 只实现 Fit(invoke 的无工具分支会调用它)。
type stubAssembler struct{}

func (stubAssembler) Assemble(context.Context, *TurnState, string) (Assembled, error) {
	return Assembled{}, nil
}

func (stubAssembler) Fit(m []llmproto.ChatMessage, _ hCommon.ContextPolicy) []llmproto.ChatMessage {
	return m
}

// stubTransport 记录调用的是 Complete 还是 Stream。
type stubTransport struct {
	completed int
	streamed  int
}

func (s *stubTransport) SimpleComplete(context.Context, dataobject.TLlm, string, string, int) (llmproto.LlmReply, error) {
	return llmproto.LlmReply{}, nil
}

func (s *stubTransport) Complete(context.Context, Provider, llmproto.LlmRequest) (llmproto.LlmReply, error) {
	s.completed++

	return llmproto.LlmReply{}, nil
}

func (s *stubTransport) Stream(context.Context, Provider, llmproto.LlmRequest, IStreamObserver) (llmproto.LlmReply, error) {
	s.streamed++

	return llmproto.LlmReply{}, nil
}
