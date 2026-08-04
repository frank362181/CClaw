/*
* User service implementation
* 访问数据库、访问内存均在同一个层级上实现：即访问数据库和访问内存被视为是读写数据的操作，
* UserService作为业务逻辑的实现，应该只关心数据的读写，而不关心数据是从数据库中获取还是从内存中获取
 */

package service

import (
	"time"

	selfCommon "my.favorite/cclawservice/common"
	myException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataaccessor"
	"my.favorite/cclawservice/model/dataobject"
	platformcommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/exception"

	"my.favorite/platformutility/common/hash"
	memoryDbCommon "my.favorite/platformutility/memory_db/common"
	"my.favorite/platformutility/webbase/middleware/service"
)

type UserService struct {
	service.BaseService

	dbAccessor dataaccessor.IAccessor   //数据库操作作为成员变量
	memoryDb   memoryDbCommon.IMemoryDB //内存数据库
}

func (service *UserService) Login(username, password string) (*dataobject.TAccount, error) {
	if len(username) == 0 || len(password) == 0 {
		return nil, exception.ParameterEmptyErrorEx("user or password is empty")
	}

	user, err := service.GetUserByNameAndPassword(username, password)
	if err != nil {
		return nil, err
	}

	user.Token, _ = hash.SHA256(user.LoginName + user.LoginPass + platformcommon.GetDatetimeUtilityInstance().GetCurrentDatetime().DatetimeStr) //根据username + password + 当前日期
	return service.saveSession(user)
}

func (service *UserService) GetUserByNameAndPassword(username, password string) (*dataobject.TAccount, error) {
	user, err := service.dbAccessor.GetUserByNameAndPassword(username, password)
	if err != nil {
		return nil, err
	}

	return user, err
}

func (service *UserService) GetUserByAccount(username string) (*dataobject.TAccount, error) {
	user, err := service.dbAccessor.GetUserByAccount(username)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (service *UserService) GetUserByToken(token string) (*dataobject.TAccount, error) {
	if len(token) == 0 {
		return nil, myException.TokenInvalidErrEx("token empty")
	}

	userInfoKey := selfCommon.UserInfoKeyPrefix + token
	userJson, err := service.memoryDb.Get(userInfoKey)
	if err != nil {
		return nil, err
	}

	var user dataobject.TAccount
	err = user.FromJsonString(userJson)
	if err != nil {
		return nil, err
	}

	userEx, err1 := service.GetUserByAccount(user.LoginName)
	if err1 != nil {
		return nil, err1
	}

	// 此时调用了API，刷新缓存
	userEx.Token = token
	return service.saveSession(userEx)
}

func (service *UserService) Logout(token string) {
	if len(token) == 0 {
		return
	}

	service.deleteSession(token)
}

// ======================== private functions & methods ========================
func (service *UserService) saveSession(user *dataobject.TAccount) (*dataobject.TAccount, error) {
	// 将用户信息写入到内存
	if service.memoryDb != nil {
		// 1. key = loginName,值为token
		userTokenKey := selfCommon.UserTokenKeyPrefix + user.LoginName
		err := service.memoryDb.Set(userTokenKey, user.Token, 0)
		if err != nil {
			return nil, err
		}

		// key=token,值为user对象
		userInfoKey := selfCommon.UserInfoKeyPrefix + user.Token
		err = service.memoryDb.Set(userInfoKey, user.ToJsonString(), time.Duration(selfCommon.MemoryDefaultExpireTime)*time.Second)
		if err != nil {
			return nil, err
		}
	}

	return user, nil
}

func (service *UserService) deleteSession(token string) {
	if service.memoryDb != nil {
		// 1. 根据token获取用户信息
		userInfoKey := selfCommon.UserInfoKeyPrefix + token
		userJson, err := service.memoryDb.Get(userInfoKey)
		if err != nil {
			return
		}

		var user dataobject.TAccount
		err = user.FromJsonString(userJson)
		if err != nil {
			return
		}

		// 2. 删除token对应的用户信息
		_, _ = service.memoryDb.Delete(userInfoKey)

		// 3. 删除loginName对应的token信息
		userTokenKey := selfCommon.UserTokenKeyPrefix + user.LoginName
		_, _ = service.memoryDb.Delete(userTokenKey)
	}
}
