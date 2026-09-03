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

	return loginService, nil
}

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

	loginSession.Token = token
	loginSession.ExpiredLength = basic.MemoryTokenExpireTime
	return loginSession, nil
}

func (service *LoginService) GetUserByToken(token string) (*request.LoginSession, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, myException.TokenInvalidErrEx("token empty")
	}

	loginSession, err := service.sessionService.Get(token)
	if err != nil {
		return nil, err
	}

	user, err := service.dbAccessor.GetUserByAccount(loginSession.LoginName)
	if err != nil {
		return nil, err
	}
	if user == nil || user.Disabled || user.Deleted {
		_ = service.sessionService.Revoke(token)
		return nil, myException.TokenInvalidErrEx("account unavailable")
	}

	// Refresh revalidates current-token ownership, so a concurrent request cannot
	// restore a token that Login or Logout has already replaced/revoked.
	if err := service.sessionService.Refresh(token, loginSession); err != nil {
		return nil, err
	}

	return loginSession, nil
}

// Logout is idempotent: an already absent token is considered successfully revoked.
func (service *LoginService) Logout(token string) error {
	return service.sessionService.Revoke(strings.TrimSpace(token))
}
