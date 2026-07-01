package database

type BaseDBConnection struct {
	pool IBaseDBPool
}

type IBaseDBConnection interface {
	GetPool() IBaseDBPool
	SetPool(pool IBaseDBPool)

	Ping() error
	Query(sql string, args ...any) error
	QueryRow(sql string, args ...any) error
	Exec(sql string, args ...any) error
	Close()
}

func (conn *BaseDBConnection) GetPool() IBaseDBPool {
	return conn.pool
}

func (conn *BaseDBConnection) SetPool(pool IBaseDBPool) {
	conn.pool = pool
}
