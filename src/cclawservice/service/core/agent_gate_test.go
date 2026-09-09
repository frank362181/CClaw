/**
* AgentGateRegistry 单元测试:并发上限、释放后放行、ctx 取消不泄漏许可。
**/

package core

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestAgentGateRegistry_LimitsConcurrency 验证同一 agent 并发运行数不超过上限,且全部请求最终完成。
func TestAgentGateRegistry_LimitsConcurrency(t *testing.T) {
	const limit = 3
	gate := &AgentGateRegistry{entries: make(map[string]*agentGateEntry)}

	var cur, max int32
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := gate.Acquire(ctx, "agent_gt", limit)
			if err != nil {
				t.Errorf("acquire error: %v", err)
				return
			}
			defer release()

			r := atomic.AddInt32(&cur, 1)
			for {
				m := atomic.LoadInt32(&max)
				if r <= m || atomic.CompareAndSwapInt32(&max, m, r) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			atomic.AddInt32(&cur, -1)
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&max); got > limit {
		t.Errorf("max concurrent = %d, want <= %d", got, limit)
	}
	if atomic.LoadInt32(&cur) != 0 {
		t.Errorf("current = %d, want 0 (all released)", cur)
	}
}

// TestAgentGateRegistry_ContextCancelReleasesWaiter 验证:上限占满时排队者会随 ctx 超时取消,
// 且取消不泄漏许可——释放后再获取可立即成功。
func TestAgentGateRegistry_ContextCancelReleasesWaiter(t *testing.T) {
	gate := &AgentGateRegistry{entries: make(map[string]*agentGateEntry)}
	ctx := context.Background()

	release0, err := gate.Acquire(ctx, "agent_ct", 1)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// 已占满:第二个获取者应排队并在超时后失败
	ctxT, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, err := gate.Acquire(ctxT, "agent_ct", 1); err == nil {
		t.Fatal("expected timeout when limit reached")
	}

	release0()

	// 许可应已归还,可立即再次获取
	release1, err := gate.Acquire(ctx, "agent_ct", 1)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	release1()
}
