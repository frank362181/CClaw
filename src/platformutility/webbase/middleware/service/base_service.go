package service

type BaseService struct {
	Name        string
	Description string
}

type IService interface {
	GetName() string
	GetDescription() string
}

func (service *BaseService) GetName() string {
	return service.Name
}

func (service *BaseService) GetDescription() string {
	return service.Description
}
