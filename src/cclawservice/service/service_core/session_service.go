/*
* file:session_service.go
* description:
*	专门用户定义和实现与用户登录相关的session,token业务逻辑处理
 */

package service_core

import (
	"crypto/subtle"
	"strings"
	"sync"
	"time"

	"my.favorite/cclawservice/controller/model"
	myException "my.favorite/cclawservice/exception"
	"my.favorite/platformutility/exception"
	memoryDbCommon "my.favorite/platformutility/memory_db/common"
	"my.favorite/platformutility/memory_db/m_redis"
	"my.favorite/platformutility/webbase/middleware/basic"

	"my.favorite/platformutility/common"
)

// DefaultAbsoluteSessionLifetime 用来限制token的绝对过期时间，不能因为一直使用而使得token永久有效
const DefaultAbsoluteSessionLifetime = 8 * 60 * 60 // 8个小时

// ISessionService owns the bidirectional session indexes and their consistency rules.
// A username has exactly one current token; an older token must never become current again.
type ISessionService interface {
	CreateOrReplace(token string, loginSession *model.LoginSession) error
	Get(token string) (*model.LoginSession, error)
	Refresh(token string, loginSession *model.LoginSession) error
	Revoke(token string) error
}

type scriptRunner interface {
	RunScript(script string, keys []string, args ...interface{}) (interface{}, error)
}

type SessionService struct {
	memoryDB memoryDbCommon.IMemoryDB
	idleTTL  time.Duration
	//absoluteLifetime time.Duration

	// The platform IMemoryDB currently has no transaction/CAS primitive. This mutex makes
	// Login/Get/Refresh/Revoke atomic relative to each other inside one service process.
	// A multi-instance deployment should replace these critical sections with Redis Lua.
	mu sync.Mutex
}

func NewSessionService(memoryDB memoryDbCommon.IMemoryDB) (*SessionService, error) {
	if memoryDB == nil {
		return nil, exception.ParameterEmptyErrorEx("session store: memory database is nil")
	}
	return &SessionService{
		memoryDB: memoryDB,
		idleTTL:  time.Duration(basic.MemoryTokenExpireTime) * time.Second,
		//absoluteLifetime: time.Duration(DefaultAbsoluteSessionLifetime) * time.Second,
	}, nil
}

func (store *SessionService) CreateOrReplace(token string, loginSession *model.LoginSession) error {
	if err := store.validateSessionInput(token, loginSession); err != nil {
		return err
	}

	now := common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp //time.Now().Unix()
	loginSession.IssuedAt = now
	loginSession.LastActiveAt = now
	loginSession.AbsoluteExpireAt = now + DefaultAbsoluteSessionLifetime //time.Now().Add(store.absoluteLifetime).Unix()

	value, err := loginSession.ToJsonString()
	if err != nil {
		return myException.TokenInvalidErrEx2("session store: encode session: %s", err.Error())
	}

	userKey := store.sessionUserKey(loginSession.LoginName)
	if runner, ok := store.memoryDB.(scriptRunner); ok {
		ttlSec := int64(store.sessionTTL(loginSession) / time.Second)
		_, err = runner.RunScript(LuaCreateOrReplace,
			[]string{userKey, store.sessionTokenKey(token)},
			token, value, ttlSec, basic.UserTokenKeyPrefix,
		)
		if err != nil {
			return myException.TokenInvalidErrEx2("session store: create session: %s", err.Error())
		}

		return nil
	}

	// 非redis,可能不支持redis lua脚本
	store.mu.Lock()
	defer store.mu.Unlock()
	oldToken, getErr := store.memoryDB.Get(userKey)
	if getErr != nil && !m_redis.IsKeyNotFound(getErr) {
		return myException.TokenInvalidErrEx2("session store: read current token: %s", getErr.Error())
	}

	newSessionKey := store.sessionTokenKey(token)
	if err := store.memoryDB.Set(newSessionKey, value, store.sessionTTL(loginSession)); err != nil {
		return myException.TokenInvalidErrEx2("session store: create session: %s", err.Error())
	}

	if err := store.memoryDB.Set(userKey, token, store.sessionTTL(loginSession)); err != nil {
		_, _ = store.memoryDB.Delete(newSessionKey)
		return myException.TokenInvalidErrEx2("session store: switch current token: %s", err.Error())
	}

	if oldToken != "" && !store.secureTokenEqual(oldToken, token) {
		// The reverse index already makes the old token invalid. Failure to eagerly
		// remove its payload must not turn a successful token switch into a false
		// login failure; the orphan is bounded by its TTL.
		_, _ = store.memoryDB.Delete(store.sessionTokenKey(oldToken))
	}

	return nil
}

func (store *SessionService) Get(token string) (*model.LoginSession, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, myException.TokenInvalidErrEx("token empty")
	}

	if runner, ok := store.memoryDB.(scriptRunner); ok {
		now := common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
		res, err := runner.RunScript(LuaGetEx,
			[]string{store.sessionTokenKey(token)},
			token, now, basic.UserTokenKeyPrefix)
		if err != nil {
			return nil, err
		}

		return store.parseLuaSessionResult(res)
	}

	// 非redis，不支持redis lua
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.getLocked(token)
}

func (store *SessionService) Refresh(token string, loginSession *model.LoginSession) error {
	if err := store.validateSessionInput(token, loginSession); err != nil {
		return err
	}

	if runner, ok := store.memoryDB.(scriptRunner); ok {
		now := common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
		res, err := runner.RunScript(LuaRefresh,
			[]string{store.sessionTokenKey(token)},
			token, now, int64(store.idleTTL/time.Second), basic.UserTokenKeyPrefix)
		if err != nil {
			return err
		}

		items, ok := res.([]interface{})
		if !ok || len(items) < 1 {
			return myException.TokenInvalidErrEx("session store: refresh session: invalid refresh token")
		}

		switch status, _ := items[0].(int64); status {
		case 2:
			return myException.TokenInvalidErrEx("refresh session: token has been replaced")
		case 3:
			return myException.TokenInvalidErrEx("session expired")
		}

		return nil
	}

	//非redis lua支持
	store.mu.Lock()
	defer store.mu.Unlock()

	current, err := store.getLocked(token)
	if err != nil {
		return err
	}

	// Preserve server-owned timestamps and avoid mutating the caller's value.
	loginSession.IssuedAt = current.IssuedAt
	loginSession.AbsoluteExpireAt = current.AbsoluteExpireAt
	loginSession.LastActiveAt = common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp //time.Now().Unix()

	value, err := loginSession.ToJsonString()
	if err != nil {
		return myException.TokenInvalidErrEx2("session store: encode refreshed session: %s", err.Error())
	}

	ttl := store.sessionTTL(loginSession)
	if ttl <= 0 {
		return store.revokeLocked(token)
	}

	if err := store.memoryDB.Set(store.sessionTokenKey(token), value, ttl); err != nil {
		return myException.TokenInvalidErrEx2("session store: refresh session: %s", err.Error())
	}

	// getLocked proved this token is current while the process-local lock is held.
	if err := store.memoryDB.Set(store.sessionUserKey(loginSession.LoginName), token, ttl); err != nil {
		return myException.TokenInvalidErrEx2("session store: refresh current token: %s", err.Error())
	}

	return nil
}

func (store *SessionService) Revoke(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil // Logout is idempotent.
	}

	if runner, ok := store.memoryDB.(scriptRunner); ok {
		_, err := runner.RunScript(LuaRevoke,
			[]string{store.sessionTokenKey(token)},
			token, basic.UserTokenKeyPrefix)
		if err != nil {
			return myException.TokenInvalidErrEx2("session store: revoke session: %s", err.Error())
		}

		return nil
	}

	// 非redis lua支持
	store.mu.Lock()
	defer store.mu.Unlock()

	return store.revokeLocked(token)
}

// ================================ private functions & methods ===================================
func (store *SessionService) getLocked(token string) (*model.LoginSession, error) {
	value, err := store.memoryDB.Get(store.sessionTokenKey(token))
	if err != nil {
		if m_redis.IsKeyNotFound(err) {
			return nil, myException.TokenInvalidErrEx("session not found")
		}
		return nil, myException.TokenInvalidErrEx2("session store: read session: %s", err.Error())
	}

	var loginSession model.LoginSession
	if err := loginSession.FromJsonString(value); err != nil {
		return nil, myException.TokenInvalidErrEx2("session store: decode session: %s", err.Error())
	}

	currentToken, err := store.memoryDB.Get(store.sessionUserKey(loginSession.LoginName))
	if err != nil {
		if m_redis.IsKeyNotFound(err) {
			_, _ = store.memoryDB.Delete(store.sessionTokenKey(token))
			return nil, myException.TokenInvalidErrEx("token has been revoked")
		}
		return nil, myException.TokenInvalidErrEx2("session store: read current token: %s", err.Error())
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

func (store *SessionService) revokeLocked(token string) error {
	value, err := store.memoryDB.Get(store.sessionTokenKey(token))
	if err != nil {
		if m_redis.IsKeyNotFound(err) {
			return nil
		}
		return myException.TokenInvalidErrEx2("session store: read session for revoke: %s", err.Error())
	}

	var loginSession model.LoginSession
	if err := loginSession.FromJsonString(value); err != nil {
		return myException.TokenInvalidErrEx2("session store: decode session for revoke: %s", err.Error())
	}

	userKey := store.sessionUserKey(loginSession.LoginName)
	currentToken, currentErr := store.memoryDB.Get(userKey)
	if currentErr != nil && !m_redis.IsKeyNotFound(currentErr) {
		return myException.TokenInvalidErrEx2("session store: read current token for revoke: %s", currentErr.Error())
	}

	// An old token must not be allowed to delete the mapping of a newer login.
	if store.secureTokenEqual(currentToken, token) {
		if _, err := store.memoryDB.Delete(userKey); err != nil {
			return myException.TokenInvalidErrEx2("session store: remove current token: %s", err.Error())
		}
	}

	if _, err := store.memoryDB.Delete(store.sessionTokenKey(token)); err != nil {
		return myException.TokenInvalidErrEx2("session store: remove session: %s", err.Error())
	}

	return nil
}

func (store *SessionService) sessionTTL(loginSession *model.LoginSession) time.Duration {
	//remaining := time.Until(time.Unix(loginSession.AbsoluteExpireAt, 0))
	remaining := common.GetDatetimeUtilityInstance().GetCurrentDatetime().Until(loginSession.AbsoluteExpireAt)
	if remaining < store.idleTTL {
		return remaining
	}

	return store.idleTTL
}

func (store *SessionService) parseLuaSessionResult(res interface{}) (*model.LoginSession, error) {
	items, ok := res.([]interface{})
	if !ok || len(items) < 2 {
		return nil, myException.TokenInvalidErrEx("session store: unexpected script result")
	}
	status, ok := items[0].(int64)
	if !ok {
		return nil, myException.TokenInvalidErrEx("session store: unexpected script status")
	}
	value, _ := items[1].(string)
	switch status {
	case 0:
		return nil, myException.TokenInvalidErrEx("session not found")
	case 2:
		return nil, myException.TokenInvalidErrEx("token has been replaced")
	case 3:
		return nil, myException.TokenInvalidErrEx("session expired")
	}
	var loginSession model.LoginSession
	if err := loginSession.FromJsonString(value); err != nil {
		return nil, myException.TokenInvalidErrEx2("session store: decode session: %s", err.Error())
	}
	return &loginSession, nil
}

func (store *SessionService) validateSessionInput(token string, loginSession *model.LoginSession) error {
	if strings.TrimSpace(token) == "" {
		return exception.ParameterEmptyErrorEx("token empty")
	}
	if loginSession == nil || strings.TrimSpace(loginSession.LoginName) == "" {
		return exception.ParameterEmptyErrorEx("session store: loginSession is invalid")
	}
	return nil
}

func (store *SessionService) sessionTokenKey(token string) string {
	return basic.UserInfoKeyPrefix + token
}

func (store *SessionService) sessionUserKey(loginName string) string {
	return basic.UserTokenKeyPrefix + strings.ToLower(strings.TrimSpace(loginName))
}

func (store *SessionService) secureTokenEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
