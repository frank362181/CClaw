package core

import (
	"errors"
	"time"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
)

type BaseDatabase struct {
	pool IBaseDBPool

	accessorType string
}

type IBaseDatabase interface {
	GetPool() IBaseDBPool
	SetPool(pool IBaseDBPool)

	GetAccessorType() string

	Open() error
	Ping() error
	Close()
}

func (conn *BaseDatabase) GetPool() IBaseDBPool {
	return conn.pool
}

func (conn *BaseDatabase) SetPool(pool IBaseDBPool) {
	conn.pool = pool
}

func (conn *BaseDatabase) GetAccessorType() string {
	return conn.accessorType
}

// OpenPoolWithRetry 建立(或重建)连接池,失败时按 DBConfig 的 retry_* 配置做"指数退避 + 抖动"重试:
//   - retry_base_delay:退避因子(基准 1 秒 × 因子)
//   - retry_max_delay: 单次等待上限(秒)
//   - retry_jitter:    抖动比例(±,用于打散并发重连)
//   - max_retries:     最大尝试次数(含首次)
//
// 连接建立失败通常是瞬时(数据库未就绪/网络抖动),启动或重连期重试可显著提升可用性;
// 配置类错误(如 DSN 解析失败)不重试,避免无谓等待。
func (conn *BaseDatabase) OpenPoolWithRetry() error {
	if conn.pool == nil {
		return exception.DatabaseErrorEx("database connection pool is nil")
	}

	cfg := conn.pool.GetDbConfig()
	policy := common.BackoffPolicy{
		BaseDelay:   common.SecondsToDuration(float64(cfg.RetryBaseDelay)), // 因子 × 1s
		MaxDelay:    common.SecondsToDuration(float64(cfg.RetryMaxDelay)),  // 单位:秒
		JitterRatio: float64(cfg.RetryJitter),
	}
	attempts := cfg.MaxRetries
	if attempts <= 0 {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			delay := policy.Delay(attempt - 1)
			logging.LogWarnF("database: open pool failed, retry %d/%d after %s, last error: %v",
				attempt, attempts, delay, lastErr)
			time.Sleep(delay)
		}

		err := conn.pool.OpenPool()
		if err == nil {
			if attempt > 1 {
				logging.LogInfoF("database: pool opened on attempt %d/%d", attempt, attempts)
			}
			return nil
		}
		lastErr = err

		if errors.Is(err, exception.ConfigError) {
			return err // 配置类错误:重试无意义
		}
	}
	return lastErr
}
