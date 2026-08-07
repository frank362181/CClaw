package service_core

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	myException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataobject"
	memoryDbCommon "my.favorite/platformutility/memory_db/common"
	"my.favorite/platformutility/webbase/middleware/basic"

	"my.favorite/platformutility/common"
)

// DefaultAbsoluteSessionLifetime 用来限制token的绝对过期时间，不能因为一直使用而使得token永久有效
const DefaultAbsoluteSessionLifetime = 8 * 60 * 60 // 8个小时

// ISessionManager owns the bidirectional session indexes and their consistency rules.
// A username has exactly one current token; an older token must never become current again.
type ISessionManager interface {
	CreateOrReplace(token string, principal *dataobject.LoginSession) error
	Get(token string) (*dataobject.LoginSession, error)
	Refresh(token string, principal *dataobject.LoginSession) error
	Revoke(token string) error
}

type MemorySessionMgr struct {
	memoryDB memoryDbCommon.IMemoryDB
	idleTTL  time.Duration
	//absoluteLifetime time.Duration

	// The platform IMemoryDB currently has no transaction/CAS primitive. This mutex makes
	// Login/Get/Refresh/Revoke atomic relative to each other inside one service process.
	// A multi-instance deployment should replace these critical sections with Redis Lua.
	mu sync.Mutex
}

var memorySessionMgr = common.GetSingleton(func() *MemorySessionMgr {
	return &MemorySessionMgr{
		memoryDB: nil,
		idleTTL:  time.Duration(basic.MemoryTokenExpireTime) * time.Second,
		//absoluteLifetime: time.Duration(DefaultAbsoluteSessionLifetime) * time.Second,
	}
}).Instance()

func GetMemorySessionMgr(memoryDB memoryDbCommon.IMemoryDB) (*MemorySessionMgr, error) {
	if memoryDB == nil {
		return nil, errors.New("session store: memory database is nil")
	}

	memorySessionMgr.memoryDB = memoryDB
	return memorySessionMgr, nil
}

func (store *MemorySessionMgr) CreateOrReplace(token string, loginSession *dataobject.LoginSession) error {
	if err := store.validateSessionInput(token, loginSession); err != nil {
		return err
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	session := *loginSession
	now := common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp //time.Now().Unix()
	session.IssuedAt = now
	session.LastActiveAt = now
	session.AbsoluteExpireAt = now + DefaultAbsoluteSessionLifetime //time.Now().Add(store.absoluteLifetime).Unix()

	value, err := session.ToJsonString()
	if err != nil {
		return fmt.Errorf("session store: encode session: %w", err)
	}

	userKey := store.sessionUserKey(session.LoginName)
	oldToken, getErr := store.memoryDB.Get(userKey)
	if getErr != nil && !common.IsKeyNotFound(getErr) {
		return fmt.Errorf("session store: read current token: %w", getErr)
	}

	newSessionKey := store.sessionTokenKey(token)
	if err := store.memoryDB.Set(newSessionKey, value, store.sessionTTL(&session)); err != nil {
		return fmt.Errorf("session store: create session: %w", err)
	}

	if err := store.memoryDB.Set(userKey, token, store.sessionTTL(&session)); err != nil {
		_, _ = store.memoryDB.Delete(newSessionKey)
		return fmt.Errorf("session store: switch current token: %w", err)
	}

	if oldToken != "" && !store.secureTokenEqual(oldToken, token) {
		// The reverse index already makes the old token invalid. Failure to eagerly
		// remove its payload must not turn a successful token switch into a false
		// login failure; the orphan is bounded by its TTL.
		_, _ = store.memoryDB.Delete(store.sessionTokenKey(oldToken))
	}

	return nil
}

func (store *MemorySessionMgr) Get(token string) (*dataobject.LoginSession, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, myException.TokenInvalidErrEx("token empty")
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	return store.getLocked(token)
}

func (store *MemorySessionMgr) Refresh(token string, loginSession *dataobject.LoginSession) error {
	if err := store.validateSessionInput(token, loginSession); err != nil {
		return err
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	current, err := store.getLocked(token)
	if err != nil {
		return err
	}

	// Preserve server-owned timestamps and avoid mutating the caller's value.
	session := *loginSession
	session.IssuedAt = current.IssuedAt
	session.AbsoluteExpireAt = current.AbsoluteExpireAt
	session.LastActiveAt = common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp //time.Now().Unix()

	value, err := session.ToJsonString()
	if err != nil {
		return fmt.Errorf("session store: encode refreshed session: %w", err)
	}

	ttl := store.sessionTTL(&session)
	if ttl <= 0 {
		return store.revokeLocked(token)
	}

	if err := store.memoryDB.Set(store.sessionTokenKey(token), value, ttl); err != nil {
		return fmt.Errorf("session store: refresh session: %w", err)
	}

	// getLocked proved this token is current while the process-local lock is held.
	if err := store.memoryDB.Set(store.sessionUserKey(session.LoginName), token, ttl); err != nil {
		return fmt.Errorf("session store: refresh current token: %w", err)
	}

	return nil
}

func (store *MemorySessionMgr) Revoke(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil // Logout is idempotent.
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	return store.revokeLocked(token)
}

// ================================ private functions & methods ===================================
func (store *MemorySessionMgr) getLocked(token string) (*dataobject.LoginSession, error) {
	value, err := store.memoryDB.Get(store.sessionTokenKey(token))
	if err != nil {
		if common.IsKeyNotFound(err) {
			return nil, myException.TokenInvalidErrEx("session not found")
		}
		return nil, fmt.Errorf("session store: read session: %w", err)
	}

	var loginSession dataobject.LoginSession
	if err := loginSession.FromJsonString(value); err != nil {
		return nil, fmt.Errorf("session store: decode session: %w", err)
	}

	currentToken, err := store.memoryDB.Get(store.sessionUserKey(loginSession.LoginName))
	if err != nil {
		if common.IsKeyNotFound(err) {
			_, _ = store.memoryDB.Delete(store.sessionTokenKey(token))
			return nil, myException.TokenInvalidErrEx("token has been revoked")
		}
		return nil, fmt.Errorf("session store: read current token: %w", err)
	}
	if !store.secureTokenEqual(currentToken, token) {
		// Remove only the stale token index. Never remove the user's newer token index.
		_, _ = store.memoryDB.Delete(store.sessionTokenKey(token))
		return nil, myException.TokenInvalidErrEx("token has been replaced")
	}

	nowTimestamp := common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	if loginSession.AbsoluteExpireAt <= nowTimestamp { //time.Now().Unix()
		_ = store.revokeLocked(token)
		return nil, myException.TokenInvalidErrEx("session expired")
	}

	return &loginSession, nil
}

func (store *MemorySessionMgr) revokeLocked(token string) error {
	value, err := store.memoryDB.Get(store.sessionTokenKey(token))
	if err != nil {
		if common.IsKeyNotFound(err) {
			return nil
		}
		return fmt.Errorf("session store: read session for revoke: %w", err)
	}

	var loginSession dataobject.LoginSession
	if err := loginSession.FromJsonString(value); err != nil {
		return fmt.Errorf("session store: decode session for revoke: %w", err)
	}

	userKey := store.sessionUserKey(loginSession.LoginName)
	currentToken, currentErr := store.memoryDB.Get(userKey)
	if currentErr != nil && !common.IsKeyNotFound(currentErr) {
		return fmt.Errorf("session store: read current token for revoke: %w", currentErr)
	}

	// An old token must not be allowed to delete the mapping of a newer login.
	if store.secureTokenEqual(currentToken, token) {
		if _, err := store.memoryDB.Delete(userKey); err != nil {
			return fmt.Errorf("session store: remove current token: %w", err)
		}
	}

	if _, err := store.memoryDB.Delete(store.sessionTokenKey(token)); err != nil {
		return fmt.Errorf("session store: remove session: %w", err)
	}

	return nil
}

func (store *MemorySessionMgr) sessionTTL(loginSession *dataobject.LoginSession) time.Duration {
	//remaining := time.Until(time.Unix(loginSession.AbsoluteExpireAt, 0))
	remaining := common.GetDatetimeUtilityInstance().GetCurrentDatetime().Until(loginSession.AbsoluteExpireAt)
	if remaining < store.idleTTL {
		return remaining
	}

	return store.idleTTL
}

func (store *MemorySessionMgr) validateSessionInput(token string, loginSession *dataobject.LoginSession) error {
	if strings.TrimSpace(token) == "" {
		return myException.TokenInvalidErrEx("token empty")
	}
	if loginSession == nil || strings.TrimSpace(loginSession.LoginName) == "" {
		return errors.New("session store: loginSession is invalid")
	}
	return nil
}

func (store *MemorySessionMgr) sessionTokenKey(token string) string {
	return basic.UserInfoKeyPrefix + token
}

func (store *MemorySessionMgr) sessionUserKey(loginName string) string {
	return basic.UserTokenKeyPrefix + strings.ToLower(strings.TrimSpace(loginName))
}

func (store *MemorySessionMgr) secureTokenEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
