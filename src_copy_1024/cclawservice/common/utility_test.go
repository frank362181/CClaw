package common

import (
	"context"
	"errors"
	"testing"
	"time"

	"my.favorite/cclawservice/harness/api/llmproto"
	"my.favorite/cclawservice/model/dataobject"
)

// 三个上下文的测试替身:活着 / 已被取消 / 已超时。
func liveCtx() context.Context { return context.Background() }

func canceledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func timedOutCtx() context.Context {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	return ctx
}

// TestClassifyInterrupt 锁住"中断来源判定"的三上下文分工与优先级。
//
// 为什么值得单测:这是**唯一**能区分"用户点中断"与"客户端断开"的地方,而且区分依据很微妙 ——
//   - 用户点中断:中断 API 取消了 streamCtx,而 reqCtx 仍然存活;
//   - 客户端断开:reqCtx 本身先被 HTTP 层取消(它会连带取消 streamCtx)。
//
// 两者在 runCtx 上都表现为 context.Canceled,只看 runCtx 必然混淆(旧实现就是这么写的)。
func TestClassifyInterrupt(t *testing.T) {
	cases := []struct {
		name      string
		reqCtx    context.Context
		runCtx    context.Context
		streamCtx context.Context
		err       error
		want      dataobject.InterruptReason
	}{
		{
			name:   "运行超时:runCtx 到期(优先级最高)",
			reqCtx: liveCtx(), runCtx: timedOutCtx(), streamCtx: liveCtx(),
			err: errors.New("boom"), want: dataobject.InterruptTimeout,
		},
		{
			name:   "客户端断开:reqCtx 已取消,即便 streamCtx 也被取消",
			reqCtx: canceledCtx(), runCtx: canceledCtx(), streamCtx: canceledCtx(),
			err: errors.New("boom"), want: dataobject.InterruptByClient,
		},
		{
			name:   "用户中断:reqCtx 存活而 streamCtx 被取消",
			reqCtx: liveCtx(), runCtx: canceledCtx(), streamCtx: canceledCtx(),
			err: errors.New("boom"), want: dataobject.InterruptByUser,
		},
		{
			name:   "上游取消:三个上下文都活着,错误是取消哨兵",
			reqCtx: liveCtx(), runCtx: liveCtx(), streamCtx: liveCtx(),
			err: llmproto.ErrRequestCanceled, want: dataobject.InterruptUpstream,
		},
		{
			name:   "其它错误:上游报错/流错误导致残缺",
			reqCtx: liveCtx(), runCtx: liveCtx(), streamCtx: liveCtx(),
			err: errors.New("upstream 500"), want: dataobject.InterruptOther,
		},
		{
			name:   "同步路径:没有 streamCtx/运行超时,传 nil 不 panic",
			reqCtx: liveCtx(), runCtx: nil, streamCtx: nil,
			err: errors.New("upstream 500"), want: dataobject.InterruptOther,
		},
		{
			name:   "同步路径 + 客户端断开",
			reqCtx: canceledCtx(), runCtx: nil, streamCtx: nil,
			err: errors.New("boom"), want: dataobject.InterruptByClient,
		},
		{
			name:   "包装过的取消哨兵(errors.Is 语义)",
			reqCtx: liveCtx(), runCtx: liveCtx(), streamCtx: liveCtx(),
			err: fmtWrap(llmproto.ErrRequestCanceled), want: dataobject.InterruptUpstream,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyInterrupt(c.reqCtx, c.runCtx, c.streamCtx, c.err); got != c.want {
				t.Fatalf("ClassifyInterrupt() = %d(%s), want %d(%s)", got, got, c.want, c.want)
			}
		})
	}
}

// TestInterruptReasonString 锁住落库枚举的人读文案(日志用)。
// 落库用数值、日志用文案 —— 文案变了不影响历史数据的可聚合性,这正是分两层的目的。
func TestInterruptReasonString(t *testing.T) {
	want := map[dataobject.InterruptReason]string{
		dataobject.InterruptNone:     "not interrupted",
		dataobject.InterruptByUser:   "interrupted by user",
		dataobject.InterruptByClient: "client disconnected",
		dataobject.InterruptTimeout:  "run timeout",
		dataobject.InterruptUpstream: "upstream stream canceled",
		dataobject.InterruptOther:    "stream/upstream error",
		dataobject.InterruptLegacy:   "interrupted (legacy, source unknown)",
	}
	for reason, text := range want {
		if got := reason.String(); got != text {
			t.Fatalf("InterruptReason(%d).String() = %q, want %q", reason, got, text)
		}
	}
}

// fmtWrap 用 %w 包一层,验证判定走的是 errors.Is 而不是指针相等。
func fmtWrap(err error) error {
	return wrapped{err: err}
}

type wrapped struct{ err error }

func (w wrapped) Error() string { return "wrapped: " + w.err.Error() }
func (w wrapped) Unwrap() error { return w.err }
