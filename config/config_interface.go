package config

// 配置项目

type ConfigurationOperator interface {
	LoadConfigFile(filename string) error

	GetStringValue(section string, key string) (string, error)
	GetIntValue(section string, key string) (int, error)
	GetBoolValue(section string, key string) (bool, error)
	GetFloatValue(section string, key string) (float32, error)
	GetDoubleValue(section string, key string) (float64, error)

	SaveConfigFile(filename string) error
}
