package service

import (
	"my.favorite/cclawservice/model/dataaccessor"
	"my.favorite/cclawservice/model/dataobject"
	memoryDbCommon "my.favorite/platformutility/memory_db/common"
	"my.favorite/platformutility/webbase/middleware/service"
)

type UserService struct {
	service.BaseService

	dbAccessor dataaccessor.IAccessor   //数据库操作作为成员变量
	memoryDb   memoryDbCommon.IMemoryDB //内存数据库
}

func (service *UserService) GetUserByNameAndPassword(username, password string) (*dataobject.T_Account, error) {
	user, err := service.dbAccessor.GetUserByNameAndPassword(username, password)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (service *UserService) GetUserByAccount(username string) (*dataobject.T_Account, error) {
	user, err := service.dbAccessor.GetUserByAccount(username)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (service *UserService) Logout() error {
	return nil
}
