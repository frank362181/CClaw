package config

// 解析结果的读写器

type ConfigurationStorage interface {
	Get(section string, key string) (string, error)
	GetWithDefault(section string, key string, defaultValue string) (string, error)
	Set(section string, key string, newValue string) error

	DeleteKey(section string, key string)
	DeleteSection(section string)
}

// 不同配置文件的解析器

type ConfigurationParser interface {
	Load(filename string) (*ConfigurationStore, error)
	Save(filename string, store *ConfigurationStore) error
}
