package config

import (
	"errors"
	"strconv"
	"strings"
)

func (c *ConfigurationHelper) GetConfigurationFileName() string {
	return c.configFileName
}

func (c *ConfigurationHelper) Extension() string {
	return c.extension
}

func (c *ConfigurationHelper) LoadConfigFile(filename string) error {
	//TODO implement me
	helper, err := LookupConfigurationHelper(filename)
	if err == nil && helper != nil && helper.configFileName != c.configFileName {
		//此时已经存在,但指向另外要给配置文件
		return errors.New("not the same configuration helper")
	}

	if helper == nil {
		return
	}

	if helper != nil && helper.loaded {
		return nil
	}

	// load the configuration file

}

func (c *ConfigurationHelper) SaveConfigFile(filename string) error {
	//TODO implement me
	panic("implement me")
}

func (c *ConfigurationHelper) GetStringValue(section string, key string) (string, error) {
	value, err := c.findKey(section, key)
	if err != nil {
		return "", err
	}

	return value, nil
}

func (c *ConfigurationHelper) GetIntValue(section string, key string) (int, error) {
	value, err := c.findKey(section, key)
	if err != nil {
		return 0, err
	}

	return strconv.Atoi(value)
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

	if configItems, ok := c.configItems[section]; ok {
		for _, configItem := range configItems {
			if configItem.keyName == key {
				return configItem.value, nil
			}
		}
	}

	return "", errors.New("key unexists")
}
