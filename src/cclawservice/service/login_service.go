package service

import (
	"strings"

	myException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataaccessor"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/service/service_core"
	"my.favorite/platformutility/common/hash"
	"my.favorite/platformutility/database/core"
	"my.favorite/platformutility/exception"
	memoryDbCommon "my.favorite/platformutility/memory_db/common"
	baseService "my.favorite/platformutility/webbase/middleware/service"
)

// LoginService contains authentication rules. Cache consistency is delegated to SessionStore.
type LoginService struct {
	baseService.BaseService

	dbAccessor   dataaccessor.IAccessor
	sessionStore service_core.ISessionManager
}

// GetLoginService builds a fresh service graph instead of mutating a package singleton.
func GetLoginService(memoryDB memoryDbCommon.IMemoryDB, db core.IBaseDatabase) (*LoginService, error) {
	if memoryDB == nil || db == nil {
		return nil, exception.ParameterEmptyErrorEx("login service: parameters are nil")
	}

	var accessor dataaccessor.IAccessor
	switch {
	case strings.EqualFold(db.GetAccessorType(), "sqldb"):
		accessor = dataaccessor.GetSQLdbAccessor(db)
	case strings.EqualFold(db.GetAccessorType(), "gormdb"):
		accessor = dataaccessor.GetGormAccessor(db)
	default:
		return nil, exception.UnsupportedExceptionErrorEx2("login service: database type[%s] is unsupported", db.GetAccessorType())
	}

	if accessor == nil {
		return nil, exception.UnsupportedExceptionErrorEx2("login service: database type[%s] is unsupported", db.GetAccessorType())
	}

	sessionStore, err := service_core.GetMemorySessionMgr(memoryDB)
	if err != nil {
		return nil, myException.TokenInvalidErrEx2("login service: token storage invalid")
	}

	loginService, err := newLoginService(accessor, sessionStore)
	if err != nil {
		return nil, err
	}

	return loginService, nil
}

func newLoginService(dbAccessor dataaccessor.IAccessor, sessionStore service_core.ISessionManager) (*LoginService, error) {
	if dbAccessor == nil {
		return nil, exception.ParameterEmptyErrorEx("login service: database accessor is nil")
	}
	if sessionStore == nil {
		return nil, exception.ParameterEmptyErrorEx("login service: session store is nil")
	}

	return &LoginService{
		BaseService: baseService.BaseService{
			Name:        "loginService",
			Description: "user authentication and session service",
		},
		dbAccessor:   dbAccessor,
		sessionStore: sessionStore,
	}, nil
}

func (service *LoginService) Login(username, password string) (*dataobject.TAccount, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" || password == "" {
		return nil, exception.ParameterEmptyErrorEx("user or password is empty")
	}

	user, err := service.dbAccessor.GetUserByNameAndPassword(username, password)
	if err != nil {
		return nil, err
	}
	if user == nil || user.Disabled {
		return nil, nil // Do not reveal whether an account exists or is disabled.
	}

	token, err := hash.GenerateRandomString()
	if err != nil {
		return nil, err
	}

	principal := &dataobject.LoginSession{
		LoginName:   user.LoginName,
		DisplayName: user.DisplayName,
	}
	if err := service.sessionStore.CreateOrReplace(token, principal); err != nil {
		return nil, err
	}

	user.LoginPass = ""
	user.Token = token
	return user, nil
}

func (service *LoginService) GetUserByToken(token string) (*dataobject.TAccount, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, myException.TokenInvalidErrEx("token empty")
	}

	principal, err := service.sessionStore.Get(token)
	if err != nil {
		return nil, err
	}

	user, err := service.dbAccessor.GetUserByAccount(principal.LoginName)
	if err != nil {
		return nil, err
	}
	if user == nil || user.Disabled {
		_ = service.sessionStore.Revoke(token)
		return nil, myException.TokenInvalidErrEx("account unavailable")
	}

	// Refresh revalidates current-token ownership, so a concurrent request cannot
	// restore a token that Login or Logout has already replaced/revoked.
	if err := service.sessionStore.Refresh(token, principal); err != nil {
		return nil, err
	}

	user.LoginPass = ""
	user.Token = token
	return user, nil
}

// Logout is idempotent: an already absent token is considered successfully revoked.
func (service *LoginService) Logout(token string) error {
	return service.sessionStore.Revoke(strings.TrimSpace(token))
}
