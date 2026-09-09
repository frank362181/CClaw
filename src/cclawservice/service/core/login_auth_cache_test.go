/*
* file:login_auth_cache_test.go
* description:
*	对认证链进程内缓存(AuthCache)的白盒单测(core 包内):
*	  命中/未命中、TTL 过期清理、登出逐出、登录替换逐出、账号缓存失效、singleflight 唤醒。
*	注:Refresh 滑动窗口(needRefresh)属 service.LoginService 私有方法,
*	  该用例见 service/login_service_test.go(避免 core→service import cycle)。
 */

package core

import (
	"sync"
	"testing"
	"time"

	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/request"
)

func testSession(loginName string) *request.LoginSession {
	now := time.Now().Unix()
	return &request.LoginSession{
		ComCode:          "100001",
		LoginName:        loginName,
		DisplayName:      "tester",
		IssuedAt:         now,
		LastActiveAt:     now,
		AbsoluteExpireAt: now + 8*60*60,
	}
}

// newTestCache 构造独立 AuthCache 实例,避免使用全局单例造成用例间状态污染。
func newTestCache() *AuthCache {
	return &AuthCache{
		tokens:    make(map[string]*tokenCacheEntry),
		userToken: make(map[string]string),
		accounts:  make(map[string]*accountCacheEntry),
		tokenLoad: newFlightTracker(),
		accLoad:   newFlightTracker(),
	}
}

func TestAuthCacheTokenHitAndMiss(t *testing.T) {
	c := newTestCache()
	tok := "token-1"
	c.TokenPut(tok, testSession("d"))

	got, ok := c.TokenGet(tok)
	if !ok {
		t.Fatalf("expected cache hit for %q", tok)
	}
	if got.LoginName != "d" || got.ComCode != "100001" {
		t.Fatalf("unexpected session copy: %+v", got)
	}

	// 未命中
	if _, ok := c.TokenGet("token-nope"); ok {
		t.Fatalf("unexpected hit for unknown token")
	}

	// 返回的是副本,外部修改不影响缓存
	got.LoginName = "hacked"
	if again, _ := c.TokenGet(tok); again.LoginName != "d" {
		t.Fatalf("cache mutated by caller copy: %s", again.LoginName)
	}
}

func TestAuthCacheTokenExpiryCleanup(t *testing.T) {
	c := newTestCache()
	tok := "token-exp"
	s := testSession("d")
	// 白盒:伪造一条已过期(缓存TTL到期)的记录
	c.tokenMu.Lock()
	c.tokens[tok] = &tokenCacheEntry{
		session:          *s,
		expiresAt:        time.Now().Unix() - 1,
		absoluteExpireAt: s.AbsoluteExpireAt,
	}
	c.tokenMu.Unlock()

	if _, ok := c.TokenGet(tok); ok {
		t.Fatalf("expired token must not hit")
	}
	c.tokenMu.RLock()
	_, still := c.tokens[tok]
	c.tokenMu.RUnlock()
	if still {
		t.Fatalf("expired token entry should be cleaned lazily")
	}
}

func TestAuthCacheTokenEvictUser(t *testing.T) {
	c := newTestCache()
	oldTok, newTok := "token-old", "token-new"
	c.TokenPut(oldTok, testSession("d"))

	// 重新登录:Login 在把新 token 入缓存前先逐出旧 token(与 Redis replace 语义对齐)
	c.EvictUser("D") // 大小写不敏感

	if _, ok := c.TokenGet(oldTok); ok {
		t.Fatalf("old token must be evicted after user re-login")
	}

	// 新 token 随后(首次 token 校验)才入缓存
	c.TokenPut(newTok, testSession("d"))
	if _, ok := c.TokenGet(newTok); !ok {
		t.Fatalf("new token must stay cached")
	}

	// 重复 EvictUser(再次登录)会逐出当前缓存中的新 token
	c.EvictUser("d")
	if _, ok := c.TokenGet(newTok); ok {
		t.Fatalf("current cached token must be evicted on re-login")
	}
}

func TestAuthCacheTokenEvictToken(t *testing.T) {
	c := newTestCache()
	tok := "token-rm"
	c.TokenPut(tok, testSession("d"))
	c.EvictToken(tok)
	if _, ok := c.TokenGet(tok); ok {
		t.Fatalf("evicted token must not hit")
	}
	// 幂等
	c.EvictToken(tok)
}

func TestAuthCacheAccount(t *testing.T) {
	c := newTestCache()
	key := "d"
	acct := &dataobject.CachedAccount{ComCode: "100001", LoginName: "d", Disabled: false}
	c.AccountPut(key, acct)

	got, ok := c.AccountGet(key)
	if !ok || got.ComCode != "100001" || got.Disabled {
		t.Fatalf("account cache miss or wrong data: ok=%v got=%+v", ok, got)
	}

	// EvictUser 会同时清空该账号的 token 与账号状态缓存
	c.EvictUser("D")
	if _, ok := c.AccountGet(key); ok {
		t.Fatalf("account cache must be invalidated after EvictUser")
	}
}

func TestAuthCacheAccountExpiry(t *testing.T) {
	c := newTestCache()
	key := "d"
	c.accMu.Lock()
	c.accounts[key] = &accountCacheEntry{
		account:   dataobject.CachedAccount{LoginName: "d"},
		expiresAt: time.Now().Unix() - 1,
	}
	c.accMu.Unlock()
	if _, ok := c.AccountGet(key); ok {
		t.Fatalf("expired account must not hit")
	}
}

func TestFlightTrackerWakeFollowers(t *testing.T) {
	ft := newFlightTracker()
	key := "k"
	done, leader := ft.begin(key)
	if !leader {
		t.Fatalf("first caller must be leader")
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fd, l := ft.begin(key)
			if l {
				t.Errorf("follower must not become leader")
			}
			<-fd
		}()
	}

	// 让 followers 先 begin 完成
	time.Sleep(50 * time.Millisecond)
	if done == nil {
		t.Fatalf("leader done channel must not be nil")
	}
	// finish 内部会 close(done) 唤醒所有 follower,并移除该 key
	ft.finish(key)
	wg.Wait()

	// key 应已从跟踪表中移除,新调用可成为 leader
	_, leader = ft.begin(key)
	if !leader {
		t.Fatalf("after finish, next caller must be leader")
	}
	ft.finish(key)
}
