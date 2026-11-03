/**
* 并发计数控制器接口
*
**/

package concurrency

import (
	"context"
	"strings"
	"time"

	pCommon "my.favorite/platformutility/common"
)

type PanicPolicy int

const (
	RecoverPanic PanicPolicy = iota //捕获 + 记 static + 计数(default)
	CrashPanic
)

const (
	// DefaultMaxInflight 全局并发上限的默认值
	// 取值依据:单进程goroutine成本低,这里的限制是"同时打包 LLM/DB的并发"
	// 这个数字足以覆盖绝大部分场景
	DefaultMaxInflight = 1024

	// DefaultDrainTimeout 关停等待在飞任务的最大超时时间
	DefaultDrainTimeout = 10

	// DefaultSlowThreshold 单个routine运行超过该时常打 WARN,unit=second
	DefaultSlowThreshold = 60
)

// IConcurrencyController 并发闸
type IConcurrencyController interface {
	// TryAcquire
	// 返回的release必须支持幂等,便于defer
	TryAcquire(ctx context.Context, key string, limit int) (release func(), err error)
	Destroy()
}

// TaskConfig task的配置信息
type TaskConfig struct {
	MaxInflight         int // 全局同时在飞的任务上限个数
	DefaultPerNameLimit int // 每个任务组最大的routine上限个数

	DrainTimeout int64 // Drain/Wait超时时间

	SlowThreshold int64 // 慢任务告警阈值
}

func (c TaskConfig) defaultTaskConfig() TaskConfig {
	if c.MaxInflight == 0 {
		c.MaxInflight = DefaultMaxInflight
	}

	if c.DrainTimeout <= 0 {
		c.DrainTimeout = DefaultDrainTimeout
	}

	if c.SlowThreshold <= 0 {
		c.SlowThreshold = DefaultSlowThreshold
	}

	return c
}

func (c TaskConfig) drainTimeout() time.Duration {
	return time.Duration(c.DrainTimeout) * time.Second
}

func (c TaskConfig) slowThreshold() time.Duration {
	return time.Duration(c.SlowThreshold) * time.Second
}

type TaskRestartPolicy struct {
	Errored    bool          // Fn返回error或panic被捕获时是否重启
	MaxRetries int           // 重试次数
	BackOff    time.Duration // 线性/指数颓靡基数
}

type RoutineSpec struct {
	Name  string // task name
	Fn    func(ctx context.Context) error
	Panic PanicPolicy //请求内一律RecoverPanic

	Limit int // 仅为Daemon生效的组属性, // 该name的最大并发; 0 = 用 Manger的默认值

	Restart TaskRestartPolicy // 仅仅Daemon有效:none / no-error(max,backoff)
}

func (r *RoutineSpec) validate() error {
	if strings.TrimSpace(r.Name) == pCommon.BlankString {
		return ErrTaskInvalidSpec.Newf("%s:Name is required", r.Name)
	}

	if r.Fn == nil {
		return ErrTaskInvalidSpec.Newf("%s:Fn is required", r.Name)
	}

	return nil
}

type RunningTask struct {
	TaskId  int64
	Name    string
	Scope   string
	Start   time.Time
	Elapsed time.Duration
}

// ========================== 统计信息 ==============================

// TaskStat 单个任务的统计信息
type TaskStat struct {
	Inflight      int64 // 当前在飞的routine数量
	MaxInflight   int64 // 在飞的routine数量历史峰值
	Started       int64 // 累计启动的数量
	Completed     int64 // 正常返回的数量
	Failed        int64 // 返回error的数量
	Panicked      int64 // panic被捕获的数量
	TotalMs       int64 // 累计耗时
	SlowestMs     int64
	SlowestTaskId int64
}

// TaskStats 所有任务的统计信息
type TaskStats struct {
	Inflight    int64
	MaxInflight int64
	Started     int64
	Completed   int64
	Failed      int64
	Panicked    int64
	Rejected    int64 // 因容量被拒的次数
	Scopes      int   // 活跃的scope数量
	ByTask      map[string]TaskStat
}

// IRoutineScope 任务作用域
// 典型用法:一组任务
type IRoutineScope interface {
	Name() string

	Go(ctx context.Context, routineSpec RoutineSpec) error
	TryGo(ctx context.Context, routineSpec RoutineSpec) error

	Wait(ctx context.Context) error

	Cancel()
	Stats() TaskStats
}

// IRoutineManager 进程级统一管理器 生命周期:
// - 正常请求:sc := NewScope() -> sc.Go() ->sc.Wait(ctx)
// - 后台启动:Daemon(routineSpec)
// - 进程关停:CancelRequest() -> Drain(ctx) -> Stop() -> Drain(ctx)
// - 一步关停:Close(ctx)
type IRoutineManager interface {
	// Go 请求内并发:绑定调用方 ctx;goroutine容量不足时**阻塞等待**,ctx结束则返回ErrNoCapacity
	// 返回error 表示未启动
	Go(ctx context.Context, routineSpec RoutineSpec) error

	// TryGo Go的非阻塞版本,拿不到名额就立即返回ErrNoCapacity
	TryGo(ctx context.Context, routineSpec RoutineSpec) error

	// NewScope 创建请求级作用域;limit<=0时用TaskConfig.DefaultPerNameLimit
	NewScope(scopeName string, limit int) IRoutineScope

	// Daemon 进程级常驻:与调用方的ctx 解绑(用自己的的根ctx),Stop时统一取消
	// 同名Daemon只能注册一次
	Daemon(routineSpec RoutineSpec) error

	// CancelScope 取消所有scope及其关联的任务
	CancelScope()

	// CancelDaemon Stop 取消所有的Daemon与在飞的任务的根ctx(不等待)
	CancelDaemon()

	// IsDraining 是否已经进入关停状态
	IsDraining() bool

	// Close = CancelRequestScoped + Stop + Drain(超时时间)
	Close(ctx context.Context) error

	// Drain 等待所有已经启动的任务结束;超时返回 ErrDrainTimeout
	Drain(ctx context.Context) error

	// AllRoutineStats 快照,返回goroutine的统计信息
	AllRoutineStats() TaskStats
	RoutineStats(scopeName string) TaskStats
}
