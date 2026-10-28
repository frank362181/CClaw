package config

import (
	"os"
	"strings"

	"my.favorite/platformutility/exception"
)

type YamlConfigParser struct{}

func (y *YamlConfigParser) Load(filename string) (IConfigurationStorage, error) {
	if strings.TrimSpace(filename) == "" {
		return nil, exception.ParameterEmptyErrorEx("filename must not be empty")
	}

	_, err := os.ReadFile(filename)
	if err != nil {
		return nil, exception.FileExtensionErrorEx("failed to read yaml file: " + err.Error())
	}

	// parse the YAML file
	//err = yaml.Unmarshal(dataBytes,)

	return nil, nil
}

func (y *YamlConfigParser) Save(filename string, storage IConfigurationStorage) error {
	panic("implement me")
}
