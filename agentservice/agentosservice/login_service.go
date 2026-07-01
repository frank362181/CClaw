package agentosservice

import (
	"my.favorite/agentservice/model"
	"my.favorite/platformutility/webbase/middleware/service"
)

type LoginService struct {
	service.BaseService
}

func (service *LoginService) Login(lq *model.LoginRequestModel) error {
	return nil
}

func (service *LoginService) Logout() error {
	return nil
}
