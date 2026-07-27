package core

type BaseDatabase struct {
	pool IBaseDBPool

	accessorType string
}

type IBaseDatabase interface {
	GetPool() IBaseDBPool
	SetPool(pool IBaseDBPool)

	GetAccessorType() string

	Open() error
	Ping() error
	Close()
}

func (conn *BaseDatabase) GetPool() IBaseDBPool {
	return conn.pool
}

func (conn *BaseDatabase) SetPool(pool IBaseDBPool) {
	conn.pool = pool
}

func (conn *BaseDatabase) GetAccessorType() string {
	return conn.accessorType
}
