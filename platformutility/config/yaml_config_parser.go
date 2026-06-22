package config

type YamlConfigParser struct{}

func (y *YamlConfigParser) Load(filename string) (*configurationStore, error) {
	panic("implement me")
}

func (y *YamlConfigParser) Save(filename string, store *configurationStore) error {
	panic("implement me")
}
