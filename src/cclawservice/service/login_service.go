/*
*
* 把所有"凭据无效"（不存在/密码错/禁用）统一成同一个哨兵错误
* 之所以在service层构建该功能：错误语义的收敛点应靠近产生地
*
**/

package service

import (
	"errors"
	"strings"

	myException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataaccessor"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/request"
	"my.favorite/cclawservice/service/service_core"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/common/hash"
	"my.favorite/platformutility/database/core"
	"my.favorite/platformutility/exception"
	memoryDbCommon "my.favorite/platformutility/memory_db/common"
	"my.favorite/platformutility/webbase/middleware/basic"
	baseService "my.favorite/platformutility/webbase/middleware/service"
)

// LoginService contains authentication rules. Cache consistency is delegated to SessionStore.
type LoginService struct {
	baseService.BaseService

	dbAccessor     dataaccessor.IAccessor
	sessionService service_core.ISessionService

	throttleService *service_core.ThrottleService

	// authCache 进程内短TTL缓存:token→session 与 账号状态,削减认证链对 Redis/SQLite 的每请求访问。
	authCache *service_core.AuthCache
}

var loginService = common.GetSingleton(func() *LoginService {
	return &LoginService{
		BaseService: baseService.BaseService{
			Name:        "loginService",
			Description: "user authentication and session service",
		},
		dbAccessor:      nil,
		sessionService:  nil,
		throttleService: nil,
	}
}).Instance()

// GetLoginService builds a fresh service graph instead of mutating a package singleton.
func GetLoginService(memoryDB memoryDbCommon.IMemoryDB, db core.IBaseDatabase) (*LoginService, error) {
	if memoryDB == nil || db == nil {
		return nil, exception.ParameterEmptyErrorEx("login service: parameters are nil")
	}

	var accessor dataaccessor.IAccessor
	accessor, err := dataaccessor.GetAccessor(db)
	if err != nil {
		return nil, err
	}

	ss, err := service_core.GetSessionService(memoryDB)
	if err != nil {
		return nil, myException.TokenInvalidErrEx2("login service: token storage invalid")
	}

	ts := service_core.NewThrottleService(memoryDB)

	loginService.dbAccessor = accessor
	loginService.sessionService = ss
	loginService.throttleService = ts
	loginService.authCache = service_core.GetAuthCache() // 重置进程内认证缓存

	return loginService, nil
}

// sessionRefreshDivisor 滑动窗口除数:仅当 距上次活跃 ≥ idleTTL/divisor 才写 Redis Refresh。
const sessionRefreshDivisor = 3

func (service *LoginService) Login(username, password string) (*request.LoginSession, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" || password == "" {
		return nil, exception.ParameterEmptyErrorEx("user or password is empty")
	}

	// 1. 锁定检查在凭据校验前：攻击期间锁定账号不落库
	if service.throttleService.IsLocked(username) {
		return nil, myException.UserPasswordUnmatchErr // 统一通用错误,不暴露锁定状态
	}

	user, err := service.dbAccessor.GetUserByNameAndPassword(username, password)
	if err != nil {
		service.throttleService.RecordFailure(username)
		if errors.Is(err, exception.UnMatchError) {
			// 统一收敛为 凭据无效，不向controller层暴露细节
			return nil, myException.UserPasswordUnmatchErr
		}
		return nil, err
	}

	if user == nil || user.Deleted || user.Disabled {
		service.throttleService.RecordFailure(username)
		return nil, myException.UserPasswordUnmatchErr
	}

	// 成功登录
	service.throttleService.Reset(username)
	token, err := hash.GenerateRandomString()
	if err != nil {
		return nil, err
	}

	loginSession := request.NewLoginSession(user.ComCode, user.LoginName, user.DisplayName)
	if err := service.sessionService.CreateOrReplace(token, loginSession); err != nil {
		return nil, err
	}

	// 登录成功:使本进程内该账号旧 token 的本地缓存立即失效(与 Redis replace 语义对齐),
	// 并清空账号状态缓存,使后续 token 校验回源读取最新账号状态。
	if service.authCache != nil {
		service.authCache.EvictUser(user.LoginName)
	}

	loginSession.Token = token
	loginSession.ExpiredLength = basic.MemoryTokenExpireTime
	return loginSession, nil
}

func (service *LoginService) GetUserByToken(token string) (*request.LoginSession, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, myException.TokenInvalidErrEx("token empty")
	}

	if service.authCache != nil {
		// 快速路径:进程内短TTL命中即返回,跳过 Redis Get / SQLite / Refresh。
		if session, ok := service.authCache.TokenGet(token); ok {
			return session, nil
		}
		// 未命中/过期:single flight 合并回源,避免缓存到期瞬间打爆 Redis/SQLite。
		return service.loadSessionWithCache(token)
	}

	// 未启用缓存时的原权威路径(防御性回退)。
	return service.loadSessionAuthoritative(token)
}

// Logout is idempotent: an already absent token is considered successfully revoked.
func (service *LoginService) Logout(token string) error {
	token = strings.TrimSpace(token)
	// 登出即逐出本进程内该 token 的会话缓存,避免已吊销 token 在 TTL 内仍被本地缓存放行。
	if service.authCache != nil {
		service.authCache.EvictToken(token)
	}
	return service.sessionService.Revoke(token)
}

// =========================== private functions & methods ===============================

// ================================ 认证链回源编排(LoginService 方法) ================================

// loadSessionWithCache 带 single flight 的权威回源:其余并发等待 leader 结果后读缓存。
func (service *LoginService) loadSessionWithCache(token string) (*request.LoginSession, error) {
	if service.authCache == nil {
		return service.loadSessionAuthoritative(token)
	}
	for {
		if session, ok := service.authCache.TokenGet(token); ok {
			return session, nil
		}
		done, leader := service.authCache.TokenLoadBegin(token) //service.authCache.tokenLoad.begin(token)
		if !leader {
			<-done
			continue // leader 已结束:回环读取缓存(leader 失败则本 goroutine 接任重试)
		}
		session, err := service.loadSessionAuthoritative(token)
		service.authCache.TokenLoadDone(token) //service.authCache.tokenLoad.finish(token)
		if err != nil {
			return nil, err
		}
		return session, nil
	}
}

// loadSessionAuthoritative 权威路径:Redis Get → 账号校验(带缓存) → 滑动窗口 Refresh → 回填本地缓存。
func (service *LoginService) loadSessionAuthoritative(token string) (*request.LoginSession, error) {
	loginSession, err := service.sessionService.Get(token)
	if err != nil {
		return nil, err
	}

	acct, exists, err := service.accountSnapshot(loginSession.LoginName)
	if err != nil {
		return nil, err
	}
	if !exists || acct.Disabled || acct.Deleted {
		// 账号不可用:立即吊销 token 并逐出本地缓存
		_ = service.sessionService.Revoke(token)
		if service.authCache != nil {
			service.authCache.EvictToken(token)
		}
		return nil, myException.TokenInvalidErrEx("account unavailable")
	}

	// Refresh 滑动窗口:距上次活跃 ≥ idleTTL/3 才写 Redis(每请求写 → 周期写)。
	if service.needRefresh(loginSession) {
		if err := service.sessionService.Refresh(token, loginSession); err != nil {
			return nil, err
		}
	}

	if service.authCache != nil {
		service.authCache.TokenPut(token, new(*loginSession))
	}
	return loginSession, nil
}

// accountSnapshot 返回账号状态(带缓存)。
func (service *LoginService) accountSnapshot(loginName string) (*dataobject.CachedAccount, bool, error) {
	if service.authCache == nil {
		return service.accountLoadAuthoritative(loginName)
	}
	key := strings.ToLower(strings.TrimSpace(loginName))
	for {
		if acct, ok := service.authCache.AccountGet(key); ok {
			return acct, true, nil
		}
		done, leader := service.authCache.AccountLoadBegin(key) //service.authCache.accLoad.begin(key)
		if !leader {
			<-done //等待leader结束
			continue
		}
		acct, exists, err := service.accountLoadAuthoritative(key)
		if err == nil && exists {
			service.authCache.AccountPut(key, acct)
		}
		service.authCache.AccountLoadDone(key) //service.authCache.accLoad.finish(key)
		if err != nil {
			return nil, false, err
		}
		if !exists {
			return nil, false, nil // 不缓存"不存在",由上层吊销 token 兜底
		}
		return acct, true, nil
	}
}

// accountLoadAuthoritative SQLite 回源(仅当本地缓存未命中/过期)。
func (service *LoginService) accountLoadAuthoritative(loginName string) (*dataobject.CachedAccount, bool, error) {
	user, err := service.dbAccessor.GetUserByAccount(loginName)
	if err != nil {
		return nil, false, err
	}
	if user == nil {
		return nil, false, nil // 约定:(nil,nil) = 未命中
	}
	acct := &dataobject.CachedAccount{
		ComCode:     user.ComCode,
		LoginName:   user.LoginName,
		DisplayName: user.DisplayName,
		Disabled:    user.Disabled,
		Deleted:     user.Deleted,
		Expired:     user.Expired,
		Version:     user.DVersion.Int64,
	}
	return acct, true, nil
}

// needRefresh 滑动窗口判定:距上次活跃超过 idleTTL/3 才真正写一次 Redis Refresh。
func (service *LoginService) needRefresh(loginSession *request.LoginSession) bool {
	if loginSession == nil {
		return false
	}
	idleSec := int64(basic.MemoryTokenExpireTime)
	window := idleSec / sessionRefreshDivisor
	now := common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	return now-loginSession.LastActiveAt >= window
}
