package config

// 解析结果的读写器

type ConfigurationStorage interface {
	Get(key string) interface{}
	Set(key string, value interface{}) error
}

// 不同配置文件的解析器

type ConfigurationParser interface {
	Load(filename string) (*ConfigurationStore, error)
	Save(filename string, store ConfigurationStore) error
}
