package core

import (
	"my.favorite/platformutility/config"
)

func GetDBPool(config *config.ConfigurationHelper) IBaseDBPool {
	dbType, _ := config.GetIntValueDefault("database", "dbtype", ConstDbTypeUnknown)
	dbHost, _ := config.GetStringValueDefault("database", "server", "")
	dbPort, _ := config.GetIntValueDefault("database", "port", 5432)
	dbName, _ := config.GetStringValueDefault("database", "dbname", "")
	dataDir, _ := config.GetStringValueDefault("database", "datadir", "")
	sslMode, _ := config.GetBoolValueDefault("database", "sslmode", false)
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

	logLevel, _ := config.GetStringValueDefault("database", "log_level", "warn")

	dbConfig := &DBConfig{
		DBType:         dbType,
		DBHost:         dbHost,
		DBPort:         dbPort,
		DataDir:        dataDir,
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
		LogLevel:       logLevel, //设置日志级别
	}

	switch dbType {
	case ConstDbTypePostgresSQL:
		return GetPGPool(dbConfig)
	case ConstDbTypeSqlite:
		return GetSqliteDBPool(dbConfig)
	default:
		break
	}

	return nil
}
