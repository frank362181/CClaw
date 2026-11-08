/**
* 对concurrency_controller类进行封装,实现基于key的并发控制器
* 即可为每个不同场景(基于key)实现并发控制
**/

package concurrency

import (
	"context"
	"sync"

	pCommon "my.favorite/platformutility/common"
)

type ConcurrentAdvance struct {
	lock sync.Mutex

	concurrentMap map[string]IConcurrencyController
}

var concurrentAdvance = pCommon.GetSingleton(func() *ConcurrentAdvance {
	return &ConcurrentAdvance{}
}).Instance()

func GetConcurrentAdvance() IConcurrencyController {
	return concurrentAdvance
}

func (ac *ConcurrentAdvance) TryAcquire(ctx context.Context, key string, limit int) (release func(), err error) {
	if limit < 1 {
		limit = 1
	}

	ac.lock.Lock()
	defer ac.lock.Unlock()

	// 懒初始化:单例由空结构体构造(map 为 nil),Destroy 之后也会回到 nil,
	if ac.concurrentMap == nil {
		ac.concurrentMap = make(map[string]IConcurrencyController)
	}

	entry := ac.concurrentMap[key]
	if entry == nil {
		entry = newConcurrent(limit)
		ac.concurrentMap[key] = entry
	}

	return entry.TryAcquire(ctx, key, limit)
}

func (ac *ConcurrentAdvance) Destroy() {
	ac.lock.Lock()
	defer ac.lock.Unlock()

	// 置空 map 而非 nil,保证 Destroy 之后仍可继续使用
	ac.concurrentMap = make(map[string]IConcurrencyController)
}

func newConcurrent(limit int) IConcurrencyController {
	return &ConcurrentController{
		max: limit,
	}
}
