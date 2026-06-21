package config

import (
	"errors"
	"strconv"
	"strings"
)

func (c *ConfigurationHelper) LoadConfiguration() error {
	//此时已经设置了配置文件
	if c.loaded {
		return nil
	}

	parser, err := GetParser(c.extension)
	if err != nil {
		return err
	}

	store, err := parser.Load(c.GetConfigurationFileName())
	if err != nil {
		return err
	}

	c.storage = store
	return nil
}

func (c *ConfigurationHelper) SaveConfiguration() error {
	parser, err := GetParser(c.extension)
	if err != nil {
		return err
	}

	return parser.Save(c.GetConfigurationFileName(), *c.storage)
}

func (c *ConfigurationHelper) GetConfigurationFileName() string {
	return c.configFileName
}

func (c *ConfigurationHelper) Extension() string {
	return c.extension
}

func (c *ConfigurationHelper) GetStringValue(section string, key string) (string, error) {
	panic("implement me")
}

func (c *ConfigurationHelper) GetIntValue(section string, key string) (int, error) {
	panic("implement me")
}

func (c *ConfigurationHelper) GetBoolValue(section string, key string) (bool, error) {
	value, err := c.findKey(section, key)
	if err != nil {
		return false, err
	}

	enabled, err := strconv.ParseBool(value)
	if err != nil {
		return false, err
	}

	return enabled, nil
}

func (c *ConfigurationHelper) GetFloatValue(section string, key string) (float32, error) {
	value, err := c.findKey(section, key)
	if err != nil {
		return 0.0, err
	}

	f32, err := strconv.ParseFloat(value, 32)
	if err != nil {
		return 0.0, err
	}

	var f32Value float32 = float32(f32)
	return f32Value, nil
}

func (c *ConfigurationHelper) GetDoubleValue(section string, key string) (float64, error) {
	value, err := c.findKey(section, key)
	if err != nil {
		return 0, err
	}

	f64, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, err
	}
	return f64, nil
}

// ================================= private functions & methods ====================================

func (c *ConfigurationHelper) findKey(section string, key string) (string, error) {
	if c.loaded {
		return "", errors.New("configuration already loaded")
	}

	section = strings.TrimSpace(section)
	key = strings.TrimSpace(key)
	if len(section) == 0 || len(key) == 0 {
		return "", errors.New("empty section or key")
	}

	//if configItems, ok := c.configItems[section]; ok {
	//	for _, configItem := range configItems {
	//		if configItem.keyName == key {
	//			return configItem.value, nil
	//		}
	//	}
	//}

	return "", errors.New("key unexists")
}
