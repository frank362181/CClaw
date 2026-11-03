package concurrency

import (
	"context"

	"my.favorite/platformutility/logging"
)

// 获取一个名额:nil表示桶不限制; block = false表示拿不到立即返回NoSlot的错误
func acquire(ctx context.Context, sem chan struct{}, block bool) error {
	if sem == nil {
		return nil
	}

	select {
	case sem <- struct{}{}:
		return nil
	default:
	}

	if !block {
		return ErrTaskNoSlot
	}

	select {
	case sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// 归还一个名额.重复释放属于编码错误:这里记录而不是静默吞掉
// 也绝不用阻塞接受(否则会永久卡死调用方)
func release(sem chan struct{}, name string) {
	if sem == nil {
		return
	}

	select {
	case <-sem:
	default:
		logging.LogErrorF("task: release without acquire detected,name=%s", name)
	}
}
