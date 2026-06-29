package config

type YamlConfigParser struct{}

func (y *YamlConfigParser) Load(filename string) (*ConfigurationStore, error) {
	panic("implement me")
}

func (y *YamlConfigParser) Save(filename string, store *ConfigurationStore) error {
	panic("implement me")
}
