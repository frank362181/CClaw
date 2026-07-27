package core

// === 定义数据库类型
const (
	ConstDbTypePostgresSQL = iota + 1 //PostgreSQL
	ConstDbTypeMysql
	ConstDbTypeSqlite
	ConstDbTypeOracle
	ConstDbTypeUnknown
)

type DBConfig struct {
	DBType       int
	DBHost       string //数据所在服务器名IP或域名
	DBPort       int
	DatabaseName string
	UserName     string
	UserPass     string

	SSLMode        bool
	MaxConnections int
	MinConnections int

	ConnectionTimeout int //最大链接超时时间

	ConnectionIdle int //最大空闲时间, 15 min
	ConnectionTTl  int //最大生存时间 30 min

	MaxRetries     int
	RetryBaseDelay float32
	RetryMaxDelay  float32
	RetryJitter    float32
}
