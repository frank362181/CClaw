package service_core

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"my.favorite/cclawservice/model/dataobject"
	memoryDbCommon "my.favorite/platformutility/memory_db/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

// ============================================================
// 测试基础设施：把 miniredis 适配为 IMemoryDB（实现 RunScript，
// 用真实 Redis Lua 解释器执行本包内的脚本）
// ============================================================

type miniredisStore struct {
	client *redis.Client
}

var _ scriptRunner = (*miniredisStore)(nil)

func (s *miniredisStore) HealthCheck()         {}
func (s *miniredisStore) PingMemoryDb() error  { return nil }
func (s *miniredisStore) IsClosed() bool       { return false }
func (s *miniredisStore) CloseMemoryDb() error { return s.client.Close() }
func (s *miniredisStore) GetIntWithDefault(string, int) (int, error) {
	return 0, errors.New("not implemented")
}

func (s *miniredisStore) Set(key string, value interface{}, expiration time.Duration) error {
	return s.client.Set(context.Background(), key, value, expiration).Err()
}

func (s *miniredisStore) Get(key string) (string, error) {
	return s.client.Get(context.Background(), key).Result()
}

func (s *miniredisStore) Delete(keys ...string) (int64, error) {
	return s.client.Del(context.Background(), keys...).Result()
}

func (s *miniredisStore) RunScript(script string, keys []string, args ...interface{}) (interface{}, error) {
	// 复用与 RedisClient.RunScript 相同的执行方式：go-redis NewScript 自动缓存 SHA
	return redis.NewScript(script).Run(context.Background(), s.client, keys, args...).Result()
}

func newTestStore(t *testing.T) (*MemorySessionMgr, *miniredisStore) {
	t.Helper()
	mr := miniredis.RunT(t)
	store := &miniredisStore{client: redis.NewClient(&redis.Options{Addr: mr.Addr()})}
	t.Cleanup(func() { _ = store.CloseMemoryDb() })

	mgr, err := NewMemorySessionMgr(store)
	if err != nil {
		t.Fatalf("NewMemorySessionMgr: %v", err)
	}
	return mgr, store
}

func principal(name string) *dataobject.LoginSession {
	return &dataobject.LoginSession{LoginName: name, DisplayName: name + "-display"}
}

// ============================================================
// Lua 路径：基本语义
// ============================================================

func TestLuaCreateGet(t *testing.T) {
	mgr, _ := newTestStore(t)
	token := "tok-1"
	if err := mgr.CreateOrReplace(token, principal("alice")); err != nil {
		t.Fatalf("CreateOrReplace: %v", err)
	}

	got, err := mgr.Get(token)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LoginName != "alice" || got.DisplayName != "alice-display" {
		t.Fatalf("unexpected session: %+v", got)
	}
	if got.IssuedAt == 0 || got.AbsoluteExpireAt != got.IssuedAt+DefaultAbsoluteSessionLifetime {
		t.Fatalf("timestamps not initialized: %+v", got)
	}
}

func TestLuaReplaceInvalidatesOldToken(t *testing.T) {
	mgr, _ := newTestStore(t)
	oldToken := "tok-old"
	newToken := "tok-new"
	if err := mgr.CreateOrReplace(oldToken, principal("alice")); err != nil {
		t.Fatalf("create old: %v", err)
	}
	if err := mgr.CreateOrReplace(newToken, principal("alice")); err != nil {
		t.Fatalf("create new: %v", err)
	}

	// 新 token 有效
	if _, err := mgr.Get(newToken); err != nil {
		t.Fatalf("Get(new) should succeed: %v", err)
	}
	// 旧 token 必须失效（不复活）
	if _, err := mgr.Get(oldToken); err == nil {
		t.Fatal("Get(old) should fail after replace")
	}
}

func TestLuaRevokeAndIdempotent(t *testing.T) {
	mgr, _ := newTestStore(t)
	token := "tok-r"
	if err := mgr.CreateOrReplace(token, principal("alice")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := mgr.Revoke(token); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := mgr.Get(token); err == nil {
		t.Fatal("Get after revoke should fail")
	}
	// 幂等：重复登出不报错
	if err := mgr.Revoke(token); err != nil {
		t.Fatalf("revoke again should be idempotent: %v", err)
	}
	if err := mgr.Revoke("   "); err != nil {
		t.Fatalf("revoke empty should be no-op: %v", err)
	}
}

func TestLuaExpiredSession(t *testing.T) {
	mgr, store := newTestStore(t)
	token := "tok-expired"

	// 手工构造一个已过绝对期的会话，直接写入 payload 键
	expired := &dataobject.LoginSession{
		LoginName:        "alice",
		DisplayName:      "alice-display",
		IssuedAt:         1,
		LastActiveAt:     1,
		AbsoluteExpireAt: 1, // 已过期
	}
	value, _ := expired.ToJsonString()
	now := time.Now().Unix()
	if err := store.Set(sessionTokenKeyForTest(token), value, time.Hour); err != nil {
		t.Fatalf("seed expired session: %v", err)
	}
	// 同步设置 user 索引
	if err := store.Set(sessionUserKeyForTest("alice"), token, time.Hour); err != nil {
		t.Fatalf("seed user index: %v", err)
	}
	_ = now

	if _, err := mgr.Get(token); err == nil {
		t.Fatal("Get on expired session should fail")
	}
	// 过期后两个键都应当被清理
	if _, err := store.Get(sessionTokenKeyForTest(token)); err == nil {
		t.Fatal("expired payload key should be cleaned")
	}
	if _, err := store.Get(sessionUserKeyForTest("alice")); err == nil {
		t.Fatal("expired user index should be cleaned")
	}
}

func TestLuaRefreshSlidesTTLAndKeepsAbsolute(t *testing.T) {
	mgr, store := newTestStore(t)
	token := "tok-refresh"
	if err := mgr.CreateOrReplace(token, principal("alice")); err != nil {
		t.Fatalf("create: %v", err)
	}
	first, _ := mgr.Get(token)

	// LastActiveAt 是秒级时间戳，直接 sleep 不可靠；改为把 lastActiveAt 篡改为过去值，
	// 证明 refresh 会真正更新它（而不是"恰好同一秒没变化"）。
	stale := *first
	stale.LastActiveAt = 1
	staleValue, _ := stale.ToJsonString()
	if err := store.Set(sessionTokenKeyForTest(token), staleValue, time.Hour); err != nil {
		t.Fatalf("seed stale lastActiveAt: %v", err)
	}

	if err := mgr.Refresh(token, principal("alice")); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	second, _ := mgr.Get(token)

	if second.AbsoluteExpireAt != first.AbsoluteExpireAt {
		t.Fatalf("refresh must preserve absolute expire: %d != %d", first.AbsoluteExpireAt, second.AbsoluteExpireAt)
	}
	if second.LastActiveAt <= 1 {
		t.Fatalf("refresh must update lastActiveAt, got %d", second.LastActiveAt)
	}
	// TTL 应被刷新（滑动窗口）
	ttl := store.client.TTL(context.Background(), sessionTokenKeyForTest(token)).Val()
	if ttl <= 0 || ttl > time.Duration(basic.MemoryTokenExpireTime)*time.Second {
		t.Fatalf("unexpected ttl after refresh: %v", ttl)
	}
}

// ============================================================
// 多实例并发：两个独立 MemorySessionMgr（模拟两个进程）共享同一 Redis
// ============================================================

func TestConcurrentLogoutVsLoginNoResurrection(t *testing.T) {
	mgr, store := newTestStore(t)
	// 第二个"实例"共享同一底层存储
	mgr2, err := NewMemorySessionMgr(store)
	if err != nil {
		t.Fatalf("NewMemorySessionMgr(2): %v", err)
	}

	loginName := "alice"
	oldToken := "tok-a"
	newToken := "tok-b"
	if err := mgr.CreateOrReplace(oldToken, principal(loginName)); err != nil {
		t.Fatalf("create old: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = mgr.Revoke(oldToken)
	}()
	go func() {
		defer wg.Done()
		_ = mgr2.CreateOrReplace(newToken, principal(loginName))
	}()

	// 两个操作都原子完成，然后确定性收尾：以 newToken 再 replace 一次，
	// 保证无论竞态顺序如何，最终 newToken 都是当前 token。
	if err := mgr.CreateOrReplace(newToken, principal(loginName)); err != nil {
		t.Fatalf("final replace: %v", err)
	}

	wg.Wait()

	// 新 token 必须有效
	if _, err := mgr.Get(newToken); err != nil {
		t.Fatalf("Get(new) after race should succeed: %v", err)
	}
	// 旧 token 绝不能复活
	if _, err := mgr.Get(oldToken); err == nil {
		t.Fatal("old token must never resurrect after logout-vs-login race")
	}
}

func TestConcurrentSameUserLoginSingleCurrentToken(t *testing.T) {
	mgr, store := newTestStore(t)
	mgr2, _ := NewMemorySessionMgr(store)

	loginName := "bob"
	const n = 8
	tokens := make([]string, n)
	for i := range tokens {
		tokens[i] = fmt.Sprintf("tok-%d", i)
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			m := mgr
			if idx%2 == 1 {
				m = mgr2
			}
			_ = m.CreateOrReplace(tokens[idx], principal(loginName))
		}(i)
	}
	wg.Wait()

	// 收敛后以最后一个 token 为准（再次 CreateOrReplace 使其成为当前）
	final := tokens[n-1]
	if err := mgr.CreateOrReplace(final, principal(loginName)); err != nil {
		t.Fatalf("final replace: %v", err)
	}

	// 只有 final 有效，其余全部失效
	if _, err := mgr.Get(final); err != nil {
		t.Fatalf("final token should be valid: %v", err)
	}
	for _, tk := range tokens[:n-1] {
		if _, err := mgr.Get(tk); err == nil {
			t.Fatalf("old token %s must be invalid after concurrent logins", tk)
		}
	}
}

func TestConcurrentGetRefreshStress(t *testing.T) {
	mgr, store := newTestStore(t)
	mgr2, _ := NewMemorySessionMgr(store)

	token := "tok-stress"
	if err := mgr.CreateOrReplace(token, principal("carol")); err != nil {
		t.Fatalf("create: %v", err)
	}

	var wg sync.WaitGroup
	var getOK, getErr atomic.Int64
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			m := mgr
			if idx%2 == 0 {
				m = mgr2
			}
			for j := 0; j < 50; j++ {
				if _, err := m.Get(token); err != nil {
					getErr.Add(1)
				} else {
					getOK.Add(1)
				}
				_ = m.Refresh(token, principal("carol"))
			}
		}(i)
	}
	wg.Wait()

	if getErr.Load() != 0 {
		t.Fatalf("Get should never fail during get/refresh stress, errs=%d", getErr.Load())
	}
	if getOK.Load() == 0 {
		t.Fatal("no successful gets")
	}
}

// ============================================================
// Lua 脚本契约：Go 侧传参必须与脚本 ARGV 约定一致
// ============================================================

// recordingRunner 记录每次 RunScript 的 KEYS/ARGV，用于契约校验
type recordingRunner struct {
	mu       sync.Mutex
	lastKeys []string
	lastArgs []interface{}
}

func (r *recordingRunner) RunScript(script string, keys []string, args ...interface{}) (interface{}, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastKeys = append([]string(nil), keys...)
	r.lastArgs = append([]interface{}(nil), args...)
	return nil, errors.New("recording only")
}

// 补齐 IMemoryDB 接口（仅用于占位，本测试只关心 RunScript 调用契约）
var _ memoryDbCommon.IMemoryDB = (*recordingRunner)(nil)

func (r *recordingRunner) HealthCheck()                                 {}
func (r *recordingRunner) PingMemoryDb() error                          { return nil }
func (r *recordingRunner) IsClosed() bool                               { return false }
func (r *recordingRunner) Set(string, interface{}, time.Duration) error { return nil }
func (r *recordingRunner) Get(string) (string, error)                   { return "", nil }
func (r *recordingRunner) GetIntWithDefault(string, int) (int, error) {
	return 0, nil
}
func (r *recordingRunner) Delete(...string) (int64, error) { return 0, nil }
func (r *recordingRunner) CloseMemoryDb() error            { return nil }

func TestCreateOrReplaceScriptContract(t *testing.T) {
	runner := &recordingRunner{}
	mgr := &MemorySessionMgr{memoryDB: runner, idleTTL: time.Duration(basic.MemoryTokenExpireTime) * time.Second}

	_ = mgr.CreateOrReplace("tok-c", principal("alice"))

	if len(runner.lastKeys) != 2 {
		t.Fatalf("createOrReplace expects 2 keys, got %d: %v", len(runner.lastKeys), runner.lastKeys)
	}
	// KEYS[1] = user key, KEYS[2] = token key
	if runner.lastKeys[0] != "token_"+"alice" {
		t.Fatalf("KEYS[1] should be user key, got %q", runner.lastKeys[0])
	}
	if runner.lastKeys[1] != "user_"+"tok-c" {
		t.Fatalf("KEYS[2] should be token key, got %q", runner.lastKeys[1])
	}
	// ARGV[1]=token ARGV[2]=json ARGV[3]=ttl ARGV[4]=prefix
	if len(runner.lastArgs) != 4 {
		t.Fatalf("createOrReplace expects 4 args, got %d: %v", len(runner.lastArgs), runner.lastArgs)
	}
	if runner.lastArgs[0] != "tok-c" {
		t.Fatalf("ARGV[1] should be token, got %v", runner.lastArgs[0])
	}
	if _, ok := runner.lastArgs[1].(string); !ok {
		t.Fatalf("ARGV[2] should be session JSON string, got %T", runner.lastArgs[1])
	}
	if runner.lastArgs[3] != basic.UserTokenKeyPrefix {
		t.Fatalf("ARGV[4] should be user-key prefix, got %v", runner.lastArgs[3])
	}
}

func TestGetScriptContract(t *testing.T) {
	runner := &recordingRunner{}
	mgr := &MemorySessionMgr{memoryDB: runner, idleTTL: time.Duration(basic.MemoryTokenExpireTime) * time.Second}

	_, _ = mgr.Get("tok-g")

	if len(runner.lastKeys) != 1 || runner.lastKeys[0] != "user_"+"tok-g" {
		t.Fatalf("get expects 1 token key, got %v", runner.lastKeys)
	}
	// ARGV[1]=token ARGV[2]=now ARGV[3]=prefix （共 3 个）
	if len(runner.lastArgs) != 3 {
		t.Fatalf("get expects 3 args, got %d: %v", len(runner.lastArgs), runner.lastArgs)
	}
	if runner.lastArgs[0] != "tok-g" {
		t.Fatalf("ARGV[1] should be token, got %v", runner.lastArgs[0])
	}
	if _, err := strconv.ParseInt(fmt.Sprintf("%v", runner.lastArgs[1]), 10, 64); err != nil {
		t.Fatalf("ARGV[2] should be numeric now, got %v", runner.lastArgs[1])
	}
	if runner.lastArgs[2] != basic.UserTokenKeyPrefix {
		t.Fatalf("ARGV[3] should be user-key prefix, got %v", runner.lastArgs[2])
	}
}

func TestRefreshScriptContract(t *testing.T) {
	runner := &recordingRunner{}
	mgr := &MemorySessionMgr{memoryDB: runner, idleTTL: time.Duration(basic.MemoryTokenExpireTime) * time.Second}

	_ = mgr.Refresh("tok-r", principal("alice"))

	if len(runner.lastArgs) != 4 {
		t.Fatalf("refresh expects 4 args, got %d: %v", len(runner.lastArgs), runner.lastArgs)
	}
	if runner.lastArgs[0] != "tok-r" {
		t.Fatalf("ARGV[1] should be token, got %v", runner.lastArgs[0])
	}
	if runner.lastArgs[3] != basic.UserTokenKeyPrefix {
		t.Fatalf("ARGV[4] should be user-key prefix, got %v", runner.lastArgs[3])
	}
}

func TestRevokeScriptContract(t *testing.T) {
	runner := &recordingRunner{}
	mgr := &MemorySessionMgr{memoryDB: runner, idleTTL: time.Duration(basic.MemoryTokenExpireTime) * time.Second}

	_ = mgr.Revoke("tok-v")

	if len(runner.lastArgs) != 2 {
		t.Fatalf("revoke expects 2 args, got %d: %v", len(runner.lastArgs), runner.lastArgs)
	}
	if runner.lastArgs[0] != "tok-v" {
		t.Fatalf("ARGV[1] should be token, got %v", runner.lastArgs[0])
	}
	if runner.lastArgs[1] != basic.UserTokenKeyPrefix {
		t.Fatalf("ARGV[2] should be user-key prefix, got %v", runner.lastArgs[1])
	}
}

// 契约测试用到的 key 构造辅助（与实际实现一致）
func sessionTokenKeyForTest(token string) string {
	return basic.UserInfoKeyPrefix + token
}

func sessionUserKeyForTest(loginName string) string {
	return basic.UserTokenKeyPrefix + loginName
}
