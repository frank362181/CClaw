package config

// 解析结果的读写器

const (
	FileIni        = "ini"
	FileProperties = "properties"
	FileYaml       = "yaml"
	FileJson       = "json"
)

// === 定义数据库类型,因为通用,所以定义为全局通用
const (
	DatabaseGormAccessing   = "gormdb"
	DatabaseCommonAccessing = "sqldb"
)

const (
	ConstDbTypePostgresSQL = iota + 1 //PostgreSQL
	ConstDbTypeMysql
	ConstDbTypeSqlite
	ConstDbTypeOracle
	ConstDbTypeUnknown
)

// default value for redis configuration
const (
	MemoryDbTypeUnknown = iota
	MemoryDbTypeRedis
	MemoryDbTypeMongoDb
	MemoryDbTypeMemcache
)

const (
	DefaultMemoryDbAddress = "127.0.0.1"
	DefaultMemoryDbPort    = 6379

	DefaultMemoryDbName = 0

	DefaultMaxPoolSize       = 20
	DefaultMinIdleConnection = 5
	DefaultMaxRetries        = 3

	DefaultDialTimeout  = 5
	DefaultReadTimeout  = 3
	DefaultWriteTimeout = 3

	HealthCheckInterval   = 10
	ReconnectionBaseDelay = 500
)

// IConfigurationStorage ========================== storage ==========================
type IConfigurationStorage interface {
	Get(section string, key string) (string, error)
	GetWithDefault(section string, key string, defaultValue string) (string, error)
	Set(section string, key string, newValue string) error

	DeleteKey(section string, key string)
	DeleteSection(section string)

	Destroy()
}

// 不同配置文件的解析器

type IConfigurationParser interface {
	Load(filename string) (IConfigurationStorage, error)
	Save(filename string, storage IConfigurationStorage) error
}
