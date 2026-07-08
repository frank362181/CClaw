package agentosservice

import (
	"my.favorite/agentservice/model/dataaccessor"
	"my.favorite/agentservice/model/dataobject"
	"my.favorite/platformutility/webbase/middleware/service"
)

type UserService struct {
	service.BaseService

	dbAccessor dataaccessor.IAccessor //数据库操作作为成员变量
}

func (service *UserService) Login(username, password string) (*dataobject.T_Account, error) {
	user, err := service.dbAccessor.GetUserByNameAndPassword(username, password)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (service *UserService) Logout() error {
	return nil
}
