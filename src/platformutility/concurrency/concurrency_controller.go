/**
* 并发计数器
*
* 目标:限制同一时刻“进行中”的数量,防止无上限占用 goroutine/上游流,制造背压。
* 现两个实例:
*   - max:允许的最大的链接数;
*   - active:当前活动的链接数量。
*
**/

package concurrency

import (
	"context"
	"sync"
	"time"

	pCommon "my.favorite/platformutility/common"
)

const (
	DefaultMaxWaitSeconds = 10 // context获取锁最大的等待时间,unit=second
)

type ConcurrentController struct {
	lock sync.Mutex

	max    int // 支持的聊天的最大链接数
	active int // 当前活跃的聊天链接数

	waiters []chan struct{} // 等待着队列(FIFO),关闭 ch 表示获得许可
}

var concurrentController = pCommon.GetSingleton(func() *ConcurrentController {
	return &ConcurrentController{
		max:    0,
		active: 0,
	}
}).Instance()

func GetConcurrentController(maxConcurrency int) IConcurrencyController {
	if maxConcurrency <= 0 {
		panic("maxConcurrency must be greater than 0")
	}
	concurrentController.max = maxConcurrency // set the concurrency
	return concurrentController
}

func (cc *ConcurrentController) MaxConcurrent() int {
	return cc.max
}

func (cc *ConcurrentController) Active() int {
	return cc.active
}

// TryAcquire 为了接口兼容,这里key,limit并无用处
func (cc *ConcurrentController) TryAcquire(ctx context.Context, key string, limit int) (release func(), err error) {
	cc.lock.Lock()

	if cc.max-cc.active > 0 {
		cc.active++

		cc.lock.Unlock()
		return cc.release, nil
	}

	if ctx == nil {
		ctx = context.Background()
	}
	waitCh := make(chan struct{})
	cc.waiters = append(cc.waiters, waitCh) //添加等待线程
	cc.lock.Unlock()

	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(DefaultMaxWaitSeconds)*time.Second)
	defer cancel()

	select {
	case <-waitCtx.Done(): //超时/取消(cancel被调用了)
		cc.lock.Lock()

		// 仍然排队 -> 取消排队
		for i, w := range cc.waiters {
			if w == waitCh {
				cc.waiters = append(cc.waiters[:i], cc.waiters[i+1:]...)
				cc.lock.Unlock()

				return nil, waitCtx.Err()
			}
		}

		// 已不在队列(释放方刚移交许可) -> 归还许可后以取消首场,避免许可泄漏
		cc.lock.Unlock()
		cc.release()
		return nil, waitCtx.Err()
	case <-waitCh:
		// 释放方已把许可移交给本等待者
		return cc.release, nil
	}
}

func (cc *ConcurrentController) Destroy() {}

// ================================ private functions & methods =============================

func (cc *ConcurrentController) release() {
	cc.lock.Lock()
	defer cc.lock.Unlock()

	if len(cc.waiters) > 0 {
		w := cc.waiters[0]
		cc.waiters = cc.waiters[1:]
		close(w) //唤醒队首,许可直接转移(active不变)
		return
	}

	cc.active--
}
