package config

import (
	"strconv"
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

func (c *ConfigurationHelper) SaveConfiguration(filename string) error {
	parser, err := GetParser(c.extension)
	if err != nil {
		return err
	}

	return parser.Save(filename, c.storage)
}

func (c *ConfigurationHelper) GetConfigurationFileName() string {
	return c.configFileName
}

func (c *ConfigurationHelper) Extension() string {
	return c.extension
}

func (c *ConfigurationHelper) GetStringValue(section string, key string) (string, error) {
	return c.storage.Get(section, key)
}

func (c *ConfigurationHelper) GetStringValueDefault(section string, key string, defaultValue string) (string, error) {
	return c.storage.GetWithDefault(section, key, defaultValue)
}

func (c *ConfigurationHelper) GetIntValue(section string, key string) (int, error) {
	defaultValue := 0
	return c.GetIntValueDefault(section, key, defaultValue)
}

func (c *ConfigurationHelper) GetIntValueDefault(section string, key string, defaultValue int) (int, error) {
	value, err := c.storage.Get(section, key)
	if err != nil {
		return defaultValue, err
	}

	intValue, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue, err
	}

	return intValue, nil
}

func (c *ConfigurationHelper) GetBoolValue(section string, key string) (bool, error) {
	defaultValue := true
	return c.GetBoolValueDefault(section, key, defaultValue)
}

func (c *ConfigurationHelper) GetBoolValueDefault(section string, key string, defaultValue bool) (bool, error) {
	value, err := c.storage.Get(section, key)
	if err != nil {
		return defaultValue, err
	}

	enabled, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue, err
	}

	return enabled, nil
}

func (c *ConfigurationHelper) GetFloatValue(section string, key string) (float32, error) {
	defaultValue := float32(0)
	return c.GetFloatValueDefault(section, key, defaultValue)
}

func (c *ConfigurationHelper) GetFloatValueDefault(section string, key string, defaultValue float32) (float32, error) {
	value, err := c.storage.Get(section, key)
	if err != nil {
		return defaultValue, err
	}

	f32, err := strconv.ParseFloat(value, 32)
	if err != nil {
		return defaultValue, err
	}

	var f32Value float32 = float32(f32)
	return f32Value, nil
}

func (c *ConfigurationHelper) GetDouble(section string, key string) (float64, error) {
	defaultValue := float64(0)
	return c.GetDoubleValueDefault(section, key, defaultValue)
}

func (c *ConfigurationHelper) GetDoubleValueDefault(section string, key string, defaultValue float64) (float64, error) {
	value, err := c.storage.Get(section, key)
	if err != nil {
		return defaultValue, err
	}

	f64, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return defaultValue, err
	}
	return f64, nil
}

// ================================= private functions & methods ====================================
