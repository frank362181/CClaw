package controller

import (
	"my.favorite/platformutility/webbase/middleware/service"
)

type BaseController struct {
	Type string //表示controller 的类别,用来控制是否需要经过Interceptor的过滤
	Name string

	Service service.IService
}

type IController interface {
	GetType() string
	GetName() string

	GetService() service.IService
}

func (bc *BaseController) GetType() string {
	return bc.Type
}

func (bc *BaseController) GetName() string {
	return bc.Name
}

func (bc *BaseController) GetService() service.IService {
	return bc.Service
}
