/**
* 并发计数控制器接口
*
**/

package concurrency

import "context"

// IConcurrencyController 并发闸
type IConcurrencyController interface {
	// TryAcquire
	// 返回的release必须支持幂等,便于defer
	TryAcquire(ctx context.Context, key string, limit int) (release func(), err error)
	Destroy()
}
