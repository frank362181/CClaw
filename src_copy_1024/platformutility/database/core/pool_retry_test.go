package core

import (
	"database/sql"
	"errors"
	"testing"

	"my.favorite/platformutility/exception"
)

// fakePool 测试用的连接池桩:OpenPool 前 failures 次返回错误,之后成功。
type fakePool struct {
	BaseDBPool
	attempts int
	failures int
	err      error
}

func (f *fakePool) OpenPool() error {
	f.attempts++
	if f.failures > 0 {
		f.failures--
		if f.err != nil {
			return f.err
		}
		return exception.DatabaseErrorEx("transient open failure")
	}
	return nil
}

func (f *fakePool) IsOpened() bool                { return true }
func (f *fakePool) GetDatabase() (*sql.DB, error) { return nil, nil }
func (f *fakePool) Ping() error                   { return nil }
func (f *fakePool) ClosePool()                    {}

// newFakeDBConfig 构造一个退避很短、无抖动的配置(基准 1ms,上限 10ms),便于确定性测试。
func newFakeDBConfig() *DBConfig {
	return &DBConfig{
		MaxRetries:     3,
		RetryBaseDelay: 0.001, // 因子 0.001 → 基准 1ms
		RetryMaxDelay:  0.01,  // 上限 10ms
		RetryJitter:    0,
	}
}

// TestOpenPoolWithRetry_SucceedsWithoutRetry 验证首次成功时不重试。
func TestOpenPoolWithRetry_SucceedsWithoutRetry(t *testing.T) {
	pool := &fakePool{BaseDBPool: BaseDBPool{DBConfig: newFakeDBConfig()}}
	db := &BaseDatabase{pool: pool}

	if err := db.OpenPoolWithRetry(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool.attempts != 1 {
		t.Errorf("attempts = %d, want 1", pool.attempts)
	}
}

// TestOpenPoolWithRetry_RetriesThenSucceeds 验证瞬时失败会按 retry_* 配置重试并最终成功。
func TestOpenPoolWithRetry_RetriesThenSucceeds(t *testing.T) {
	pool := &fakePool{BaseDBPool: BaseDBPool{DBConfig: newFakeDBConfig()}, failures: 2}
	db := &BaseDatabase{pool: pool}

	if err := db.OpenPoolWithRetry(); err != nil {
		t.Fatalf("should succeed after retries: %v", err)
	}
	if pool.attempts != 3 {
		t.Errorf("attempts = %d, want 3", pool.attempts)
	}
}

// TestOpenPoolWithRetry_GivesUpAfterMaxAttempts 验证尝试次数受 max_retries 限制。
func TestOpenPoolWithRetry_GivesUpAfterMaxAttempts(t *testing.T) {
	pool := &fakePool{BaseDBPool: BaseDBPool{DBConfig: newFakeDBConfig()}, failures: 99}
	db := &BaseDatabase{pool: pool}

	if err := db.OpenPoolWithRetry(); err == nil {
		t.Fatal("expected error after exhausting attempts")
	}
	if pool.attempts != 3 {
		t.Errorf("attempts = %d, want 3 (max_retries)", pool.attempts)
	}
}

// TestOpenPoolWithRetry_NoRetryOnConfigError 验证配置类错误(DSN 非法等)不重试。
func TestOpenPoolWithRetry_NoRetryOnConfigError(t *testing.T) {
	pool := &fakePool{
		BaseDBPool: BaseDBPool{DBConfig: newFakeDBConfig()},
		failures:   99,
		err:        exception.ConfigErrorEx("bad dsn"),
	}
	db := &BaseDatabase{pool: pool}

	err := db.OpenPoolWithRetry()
	if err == nil {
		t.Fatal("expected config error")
	}
	if !errors.Is(err, exception.ConfigError) {
		t.Errorf("error should be ConfigError, got: %v", err)
	}
	if pool.attempts != 1 {
		t.Errorf("attempts = %d, want 1 (config error must not retry)", pool.attempts)
	}
}

// TestOpenPoolWithRetry_ZeroMaxRetriesTriesOnce 验证 max_retries<=0 时至少尝试一次。
func TestOpenPoolWithRetry_ZeroMaxRetriesTriesOnce(t *testing.T) {
	cfg := newFakeDBConfig()
	cfg.MaxRetries = 0
	cfg.RetryBaseDelay = 0
	cfg.RetryMaxDelay = 0
	pool := &fakePool{BaseDBPool: BaseDBPool{DBConfig: cfg}, failures: 99}
	db := &BaseDatabase{pool: pool}

	if err := db.OpenPoolWithRetry(); err == nil {
		t.Fatal("expected error")
	}
	if pool.attempts != 1 {
		t.Errorf("attempts = %d, want 1", pool.attempts)
	}
}

// TestOpenPoolWithRetry_NilPool 验证未注入连接池时返回错误而非 panic。
func TestOpenPoolWithRetry_NilPool(t *testing.T) {
	db := &BaseDatabase{}
	if err := db.OpenPoolWithRetry(); err == nil {
		t.Fatal("expected error for nil pool")
	}
}
