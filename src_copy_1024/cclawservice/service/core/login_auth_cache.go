/*
* file:login_auth_cache.go
* description:
*	认证链进程内缓存与降频：
*	  1. token→session 短TTL缓存(命中即跳过 Redis Get + Refresh);
*	  2. Refresh 由"每请求写"降为滑动窗口(距上次活跃 ≥ idleTTL/3 才写);
*	  3. GetUserByAccount 结果按 账号+版本 短TTL缓存(账号禁用/删除传播上界=TTL)。
*	语义与安全：
*	  - 本缓存为进程内、单实例视角;Redis 仍是权威会话存储与多实例一致源。
*	  - 命中缓存时允许 ≤ TTL 的吊销/替换延迟(登出在本进程内即时逐出)。
*	  - 缓存过期瞬间用 single flight 合并回源,避免打爆 Redis/SQLite。
 */

package core

import (
	"strings"
	"sync"
	"time"

	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/request"
	"my.favorite/platformutility/common"
)

const (
	// tokenAuthCacheTTL token→session 进程内短TTL:命中即不再访问 Redis。
	tokenAuthCacheTTL = 30 * time.Second

	// accountAuthCacheTTL 账号状态缓存 TTL:账号禁用/删除/过期等变更的传播上界。
	accountAuthCacheTTL = 15 * time.Second
)

type tokenCacheEntry struct {
	session          request.LoginSession
	expiresAt        int64 // Unix 秒,缓存自身 TTL 截止
	absoluteExpireAt int64 // Unix 秒,会话绝对过期(不晚于该时刻)
}

type accountCacheEntry struct {
	account   dataobject.CachedAccount
	expiresAt int64 // Unix 秒
}

// flightCall / flightTracker:自研 single flight,合并"缓存过期瞬间"的并发回源。
type flightCall struct {
	done chan struct{}
}

type flightTracker struct {
	mu sync.Mutex
	m  map[string]*flightCall
}

func newFlightTracker() *flightTracker {
	return &flightTracker{m: make(map[string]*flightCall)}
}

// begin 返回(done,isLeader)。leader 负责回源并在结束后调用 finish。
func (f *flightTracker) begin(key string) (<-chan struct{}, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if c, ok := f.m[key]; ok {
		return c.done, false
	}
	c := &flightCall{done: make(chan struct{})} //当前的请求就是leader
	f.m[key] = c
	return c.done, true
}

func (f *flightTracker) finish(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.m[key]; ok {
		close(c.done)
		delete(f.m, key)
	}
}

// AuthCache 汇总 token 会话缓存与账号缓存。
type AuthCache struct {
	tokenMu   sync.RWMutex
	tokens    map[string]*tokenCacheEntry // key: token
	userToken map[string]string           // loginName(lower) → 当前缓存的 token(用于登录替换时逐出)

	accMu    sync.RWMutex
	accounts map[string]*accountCacheEntry // key: loginName(lower)

	tokenLoad *flightTracker
	accLoad   *flightTracker
}

var authCache = common.GetSingleton(func() *AuthCache {
	return &AuthCache{
		tokens:    make(map[string]*tokenCacheEntry),
		userToken: make(map[string]string),
		accounts:  make(map[string]*accountCacheEntry),

		tokenLoad: newFlightTracker(),
		accLoad:   newFlightTracker(),
	}
}).Instance()

func GetAuthCache() *AuthCache {
	return authCache
}

func (a *AuthCache) TokenLoadBegin(key string) (<-chan struct{}, bool) {
	return a.tokenLoad.begin(key)
}

func (a *AuthCache) TokenLoadDone(key string) {
	a.tokenLoad.finish(key)
}

func (a *AuthCache) AccountLoadBegin(key string) (<-chan struct{}, bool) {
	return a.accLoad.begin(key)
}

func (a *AuthCache) AccountLoadDone(key string) {
	a.accLoad.finish(key)
}

// ================================ token→session 缓存 ================================

// TokenGet 命中且未过期则返回会话副本;过期(含绝对过期)自动清除并返回未命中。
func (a *AuthCache) TokenGet(token string) (*request.LoginSession, bool) {
	a.tokenMu.RLock()
	e, ok := a.tokens[token]
	a.tokenMu.RUnlock()
	if !ok {
		return nil, false
	}
	now := common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp //time.Now().Unix()
	if now >= e.expiresAt || now >= e.absoluteExpireAt {
		a.tokenMu.Lock()
		if cur, curOK := a.tokens[token]; curOK && (now >= cur.expiresAt || now >= cur.absoluteExpireAt) {
			a.removeTokenLocked(token, cur.session.LoginName)
		}
		a.tokenMu.Unlock()
		return nil, false
	}
	return new(e.session), true
}

// TokenPut 写入会话缓存,并登记 userToken 反查(供登录替换/登出逐出)。
func (a *AuthCache) TokenPut(token string, session *request.LoginSession) {
	if session == nil {
		return
	}
	now := common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	entry := &tokenCacheEntry{
		session:          *session,
		expiresAt:        now + int64(tokenAuthCacheTTL/time.Second),
		absoluteExpireAt: session.AbsoluteExpireAt,
	}
	key := strings.ToLower(strings.TrimSpace(session.LoginName))

	a.tokenMu.Lock()
	a.tokens[token] = entry
	a.userToken[key] = token
	a.tokenMu.Unlock()
}

// ================================ 账号状态缓存 ================================

// AccountGet 命中且未过期则返回账号快照。只缓存"存在"的账号(不缓存负结果)。
func (a *AuthCache) AccountGet(key string) (*dataobject.CachedAccount, bool) {
	a.accMu.RLock()
	e, ok := a.accounts[key]
	a.accMu.RUnlock()
	if !ok {
		return nil, false
	}

	now := common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	if now >= e.expiresAt {
		a.accMu.Lock()
		if cur, curOK := a.accounts[key]; curOK && common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp >= cur.expiresAt {
			delete(a.accounts, key)
		}
		a.accMu.Unlock()
		return nil, false
	}
	return new(e.account), true
}

func (a *AuthCache) AccountPut(key string, account *dataobject.CachedAccount) {
	if account == nil {
		return
	}
	entry := &accountCacheEntry{
		account:   *account,
		expiresAt: common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp + int64(accountAuthCacheTTL/time.Second),
	}
	a.accMu.Lock()
	a.accounts[key] = entry
	a.accMu.Unlock()
}

// EvictToken 登出/账号不可用/失效时逐出单个 token(幂等)。
func (a *AuthCache) EvictToken(token string) {
	a.tokenMu.Lock()
	if e, ok := a.tokens[token]; ok {
		a.removeTokenLocked(token, e.session.LoginName)
	}
	a.tokenMu.Unlock()
}

// EvictUser 该账号重新登录后逐出旧 token 的本地缓存(与 Redis replace 语义对齐)。
func (a *AuthCache) EvictUser(loginName string) {
	key := strings.ToLower(strings.TrimSpace(loginName))

	a.tokenMu.Lock()
	if oldToken, ok := a.userToken[key]; ok {
		if e, eOK := a.tokens[oldToken]; eOK {
			a.removeTokenLocked(oldToken, e.session.LoginName)
		} else {
			delete(a.userToken, key)
		}
	}
	a.tokenMu.Unlock()

	// 账号状态变化(登录/禁用等)后清除,使下一次校验回源读取。
	a.accMu.Lock()
	defer a.accMu.Unlock()
	delete(a.accounts, key)
}

// ========================== private functions & methods ==============================
func (a *AuthCache) removeTokenLocked(token, loginName string) {
	delete(a.tokens, token)
	key := strings.ToLower(strings.TrimSpace(loginName))
	if a.userToken[key] == token {
		delete(a.userToken, key)
	}
}
