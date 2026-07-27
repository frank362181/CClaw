package config

import "sync"

// 解析结果的读写器

type ConfigurationStore struct {
	mutex sync.RWMutex
	data  map[string]map[string]string //存储配置信息
}

type ConfigurationHelper struct {
	configFileName string //配置文件名称
	extension      string //配置文件类型
	loaded         bool   //是否已经加载

	storage *ConfigurationStore
}

type IConfigurationStorage interface {
	Get(section string, key string) (string, error)
	GetWithDefault(section string, key string, defaultValue string) (string, error)
	Set(section string, key string, newValue string) error

	DeleteKey(section string, key string)
	DeleteSection(section string)
}

// 不同配置文件的解析器

type IConfigurationParser interface {
	Load(filename string) (*ConfigurationStore, error)
	Save(filename string, store *ConfigurationStore) error
}
