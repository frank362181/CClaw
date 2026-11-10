/**
* TaskManager实例化的入口类
**/

package concurrency

import (
	"sync"
	"sync/atomic"
)

// NewTaskManager 创建一个不受全局单利影响的独立的 TaskManager
func NewTaskManager(taskCfg TaskConfig) *TaskManager {
	return newTaskManager(taskCfg)
}

// DefaultTaskManager 返回进程级默认的TaskManager实例
func DefaultTaskManager() *TaskManager {
	return taskManagerInstance
}

var taskManagerOverride atomic.Pointer[IRoutineManager]

// SwapTaskManager 把全局实例临时替换为 m,返回还原函数(幂等,可安全 defer)。
//
// 仅用于测试:
//
//	m := concurrency.NewTaskManager(concurrency.TaskConfig{DrainTimeout: 1})
//	defer concurrency.SwapTaskManager(m)()
//
// 注意:替换是**进程级**的,不要放进 t.Parallel() 的用例里。
func SwapTaskManager(m IRoutineManager) (restore func()) {
	var prev IRoutineManager

	if p := taskManagerOverride.Load(); p != nil {
		prev = *p
	}

	if m == nil {
		taskManagerOverride.Store(nil)
	} else {
		taskManagerOverride.Store(&m)
	}

	var once sync.Once

	return func() {
		once.Do(func() {
			if prev == nil {
				taskManagerOverride.Store(nil)
				return
			}

			taskManagerOverride.Store(&prev)
		})
	}
}
