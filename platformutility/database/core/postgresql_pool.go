package core

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
)

type PostgresqlPool struct {
	BaseDBPool
	pgxPool *pgxpool.Pool

	db *sql.DB
}

func (pool *PostgresqlPool) IsOpened() bool {
	return pool.pgxPool != nil
}

func (pool *PostgresqlPool) OpenPool() error {
	pool.ClosePool() //double check,confirm to close the pool

	sslMode := "disable"
	if pool.DBConfig.SSLMode {
		sslMode = "require"
	}

	configString := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		pool.DBConfig.UserName, pool.DBConfig.UserPass, pool.DBConfig.DBHost, pool.DBConfig.DBPort,
		pool.DBConfig.DatabaseName, sslMode)

	poolCfg, err := pgxpool.ParseConfig(configString)
	if err != nil {
		return exception.ConfigErrorEx(err.Error())
	}

	// 连接池配置
	poolCfg.MaxConns = int32(pool.DBConfig.MaxConnections)
	poolCfg.MinConns = int32(pool.DBConfig.MinConnections)
	poolCfg.MaxConnIdleTime = time.Duration(pool.DBConfig.ConnectionIdle) * time.Second
	poolCfg.MaxConnLifetime = time.Duration(pool.DBConfig.ConnectionTTl) * time.Second

	// 链接超时,获取链接等待时间
	poolCfg.ConnConfig.ConnectTimeout = time.Duration(pool.DBConfig.ConnectionTimeout) * time.Second

	// 健康检查时间间隔
	poolCfg.HealthCheckPeriod = 30 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool.pgxPool, err = pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return exception.DatabaseErrorEx(err.Error())
	}

	// testing database
	err = pool.Ping()
	if err != nil {
		return exception.DatabaseErrorEx(err.Error())
	}

	pool.db = stdlib.OpenDBFromPool(pool.pgxPool)

	logging.LogInfo("PostgreSQL pool established: " + pool.DBConfig.DBHost + ":" + pool.DBConfig.DatabaseName)
	return nil
}

func (pool *PostgresqlPool) GetDatabase() (*sql.DB, error) {
	if pool.IsOpened() {
		if pool.db == nil {
			pool.db = stdlib.OpenDBFromPool(pool.pgxPool)
		}

		return pool.db, nil
	}

	return nil, exception.DatabaseErrorEx("Database is not opened")
}

func (pool *PostgresqlPool) Ping() error {
	if pool.pgxPool == nil {
		return exception.DatabaseErrorEx("can not ping database,pool is not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := pool.pgxPool.Ping(ctx)
	if err != nil {
		return exception.DatabaseErrorEx(err.Error())
	}

	return nil
}

func (pool *PostgresqlPool) ClosePool() {
	if pool.db != nil {
		_ = pool.db.Close()
	}

	if pool.pgxPool != nil {
		pool.pgxPool.Close()
		pool.pgxPool = nil
	}
}

// ======================== private functions & methods =====================================
