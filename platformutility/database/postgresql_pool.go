package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"my.favorite/platformutility/config"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
)

type PostgresqlPool struct {
	BaseDBPool

	pgxPool *pgxpool.Pool
}

func (pool *PostgresqlPool) InitDbPool(config *config.ConfigurationHelper) error {
	if config == nil {
		return exception.ParameterEmptyErrorEx("database config unexists")
	}
	dbHost, _ := config.GetStringValueDefault("database", "server", "")
	dbPort, _ := config.GetIntValueDefault("database", "port", 5432)
	dbName, _ := config.GetStringValueDefault("database", "dbname", "")
	sslMode, _ := config.GetStringValueDefault("database", "sslmode", "false")
	dbUser, _ := config.GetStringValueDefault("database", "username", "")
	dbPassword, _ := config.GetStringValueDefault("database", "password", "")
	minCons, _ := config.GetIntValueDefault("database", "min_connections", 5)
	maxCons, _ := config.GetIntValueDefault("database", "max_connections", 10)
	connectTimeout, _ := config.GetIntValueDefault("database", "connection_timeout", 30)
	connectionIdle, _ := config.GetIntValueDefault("database", "connection_idle", 900)
	connectionTTl, _ := config.GetIntValueDefault("database", "connection_ttl", 1800)
	pool.dbConfig = &DBConfig{
		DBHost:         dbHost,
		DBPort:         dbPort,
		DatabaseName:   dbName,
		UserName:       dbUser,
		UserPass:       dbPassword,
		SSLMode:        sslMode,
		MaxConnections: maxCons,
		MinConnections: minCons,

		ConnectionTimeout: connectTimeout,
		ConnectionIdle:    connectionIdle,
		ConnectionTTl:     connectionTTl,
	}

	if pool.dbConfig == nil {
		return exception.ConfigErrorEx("can not config for PostgreSQL")
	}

	configString := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		pool.dbConfig.UserName, pool.dbConfig.UserPass, pool.dbConfig.DBHost, pool.dbConfig.DBPort,
		pool.dbConfig.DatabaseName, pool.dbConfig.SSLMode)

	poolCfg, err := pgxpool.ParseConfig(configString)
	if err != nil {
		return exception.ConfigErrorEx(err.Error())
	}

	// 连接池配置
	poolCfg.MaxConns = int32(pool.dbConfig.MaxConnections)
	poolCfg.MinConns = int32(pool.dbConfig.MinConnections)
	poolCfg.MaxConnIdleTime = time.Duration(pool.dbConfig.ConnectionIdle) * time.Second
	poolCfg.MaxConnLifetime = time.Duration(pool.dbConfig.ConnectionTTl) * time.Second

	// 链接超时,获取链接等待时间
	poolCfg.ConnConfig.ConnectTimeout = time.Duration(pool.dbConfig.ConnectionTimeout) * time.Second

	// 健康检查时间间隔
	poolCfg.HealthCheckPeriod = 30 * time.Second

	//链接数据库
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool.pgxPool, err = pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return exception.DatabaseErrorEx(err.Error())
	}

	// testing database
	if err := pool.pgxPool.Ping(ctx); err != nil {
		pool.pgxPool.Close()
		return exception.DatabaseErrorEx("failed to ping database")
	}

	logging.LogInfo("PostgreSQL pool established: " + pool.dbConfig.DBHost + ":" + pool.dbConfig.DatabaseName)
	return nil
}

func (pool *PostgresqlPool) CloseDbPool() {
	if pool.pgxPool != nil {
		pool.pgxPool.Close()
	}
}
