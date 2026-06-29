package config

type PropertiesConfigParser struct{}

func (p *PropertiesConfigParser) Load(filename string) (*ConfigurationStore, error) {
	panic("implement me")
}

func (p *PropertiesConfigParser) Save(filename string, store *ConfigurationStore) error {
	panic("implement me")
}
