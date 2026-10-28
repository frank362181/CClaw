package config

type PropertiesConfigParser struct{}

func (p *PropertiesConfigParser) Load(filename string) (IConfigurationStorage, error) {
	panic("implement me")
}

func (p *PropertiesConfigParser) Save(filename string, storage IConfigurationStorage) error {
	panic("implement me")
}
