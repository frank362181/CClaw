package mstorage

import (
	"sync"
	"testing"
	"time"
)

// TestSessionLocksSerializesSameKey 同一会话键必须互斥:
// 会话内 seq(轮次序号)与历史文件 index 都是"读最大值 + 1",并发进入会分配重复值。
func TestSessionLocksSerializesSameKey(t *testing.T) {
	locks := GetStorageLocks()

	const goroutines = 8
	counter := 0
	maxInCritical := 0

	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			unlock := locks.Lock("session-serialize")
			defer unlock()

			mu.Lock()
			counter++
			if counter > maxInCritical {
				maxInCritical = counter
			}
			mu.Unlock()

			time.Sleep(2 * time.Millisecond) // 拉长临界区,放大并发问题

			mu.Lock()
			counter--
			mu.Unlock()
		}()
	}
	wg.Wait()

	if maxInCritical != 1 {
		t.Errorf("同一键的临界区最多只应有 1 个 goroutine,实际 %d", maxInCritical)
	}
}

// TestSessionLocksIndependentKeys 不同会话键之间不应互相阻塞
// (否则一个长会话会串死所有其他会话)。
func TestSessionLocksIndependentKeys(t *testing.T) {
	locks := GetStorageLocks()

	unlockA := locks.Lock("session-a")
	defer unlockA()

	done := make(chan struct{})
	go func() {
		defer close(done)
		unlockB := locks.Lock("session-b")
		defer unlockB()
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("不同会话键之间发生了阻塞")
	}
}

// TestSessionLocksReleaseRemovesEntry 释放后必须回收锁对象,否则 entries 会随会话数无限膨胀。
func TestSessionLocksReleaseRemovesEntry(t *testing.T) {
	locks := GetStorageLocks()
	key := "session-recycle"

	unlock := locks.Lock(key)
	unlock()

	locks.mu.Lock()
	_, exists := locks.entries[key]
	locks.mu.Unlock()

	if exists {
		t.Errorf("锁释放后应回收 entries[%q],避免内存膨胀", key)
	}
}

// TestSessionLocksReentrantAfterRelease 释放后可再次获取(引用计数未把条目永久锁死)。
func TestSessionLocksReentrantAfterRelease(t *testing.T) {
	locks := GetStorageLocks()
	key := "session-reentrant"

	for i := 0; i < 3; i++ {
		unlock := locks.Lock(key)

		acquired := make(chan struct{})
		go func() {
			inner := locks.Lock(key)
			inner()
			close(acquired)
		}()

		select {
		case <-acquired:
			t.Fatalf("第 %d 轮:持有锁期间不应能再次获取同一键", i)
		case <-time.After(20 * time.Millisecond):
		}

		unlock()

		select {
		case <-acquired:
		case <-time.After(time.Second):
			t.Fatalf("第 %d 轮:释放后应能重新获取同一键", i)
		}
	}
}
