package config

// 解析结果的读写器

const (
	FileIni        = "ini"
	FileProperties = "properties"
	FileYaml       = "yaml"
	FileJson       = "json"
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
