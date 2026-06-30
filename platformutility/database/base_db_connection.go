package database

type BaseDBConnection struct {
	pool IBaseDBPool
}

type IBaseDBConnection interface {
	ConnectDatabase() error
	CloseDatabase() error
}

func (conn *BaseDBConnection) ConnectDatabase() error {
	return nil
}

func (conn *BaseDBConnection) CloseDatabase() error {
	return nil
}
