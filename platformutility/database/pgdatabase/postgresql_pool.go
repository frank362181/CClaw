package pgdatabase

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"my.favorite/platformutility/config"
	"my.favorite/platformutility/database"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
)

type PostgresqlPool struct {
	database.BaseDBPool

	pgxPool *pgxpool.Pool
}

func (pool *PostgresqlPool) InitPool(config *config.ConfigurationHelper) error {
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

	maxRetries, _ := config.GetIntValueDefault("database", "max_retries", 5)
	retryBaseDelay, _ := config.GetFloatValueDefault("database", "retry_base_delay", 1.0)
	retryMaxDelay, _ := config.GetFloatValueDefault("database", "retry_max_delay", 60.0)
	retryJitter, _ := config.GetFloatValueDefault("database", "retry_jitter", 0.1)

	pool.DBConfig = &database.DBConfig{
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

		MaxRetries:     maxRetries,
		RetryBaseDelay: retryBaseDelay,
		RetryMaxDelay:  retryMaxDelay,
		RetryJitter:    retryJitter,
	}

	if pool.DBConfig == nil {
		return exception.ConfigErrorEx("can not config for PostgreSQL")
	}

	return nil
}

func (pool *PostgresqlPool) OpenPool() error {
	if pool.Opened {
		return nil
	}

	pool.ClosePool() //double check,confirm to close the pool

	configString := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		pool.DBConfig.UserName, pool.DBConfig.UserPass, pool.DBConfig.DBHost, pool.DBConfig.DBPort,
		pool.DBConfig.DatabaseName, pool.DBConfig.SSLMode)

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

	pool.Opened = true

	logging.LogInfo("PostgreSQL pool established: " + pool.DBConfig.DBHost + ":" + pool.DBConfig.DatabaseName)
	return nil
}

func (pool *PostgresqlPool) Ping() error {
	if pool.pgxPool == nil || !pool.Opened {
		return exception.DatabaseErrorEx("can not ping database,pool is not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := pool.pgxPool.Ping(ctx)
	if err != nil {
		pool.Opened = false
		return exception.DatabaseErrorEx(err.Error())
	}

	return nil
}

func (pool *PostgresqlPool) ClosePool() {
	if pool.pgxPool != nil {
		pool.pgxPool.Close()
		pool.pgxPool = nil
		pool.Opened = false
	}
}
