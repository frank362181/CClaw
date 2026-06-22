package config

type PropertiesConfigParser struct{}

func (p *PropertiesConfigParser) Load(filename string) (*configurationStore, error) {
	panic("implement me")
}

func (p *PropertiesConfigParser) Save(filename string, store *configurationStore) error {
	panic("implement me")
}
