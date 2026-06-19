package config

import (
	"CClaw/common"
	"errors"
	"strconv"
	"strings"
	"sync"
)

type configItem struct {
	keyName string
	value   string
}

type ConfigurationHelper struct {
	configFileName string //配置文件名称
	extension      string //配置文件类型
	loaded         bool   //是否已经加载

	configItems map[string][]configItem //存放配置项目
}

var (
	mutex                sync.RWMutex
	configurationHelpers = make(map[string]*ConfigurationHelper)
)

// GetConfigurationWrapper singleton,应用级别
func GetConfigurationWrapper(filename string) (*ConfigurationHelper, error) {
	if len(strings.TrimSpace(filename)) == 0 {
		return nil, errors.New("filename must not be empty")
	}
	tmpFileName := strings.TrimSpace(filename)

	mutex.RLock()
	defer mutex.RUnlock()
	if helper, ok := configurationHelpers[tmpFileName]; ok {
		return helper, nil
	}

	// 不存在 create a new
	mutex.Lock()
	defer mutex.Unlock()

	//double check
	if helper, ok := configurationHelpers[tmpFileName]; ok {
		return helper, nil
	}

	ext := common.GetOSUtilityInstance().GetExtension(tmpFileName)
	if ext == "" {
		return nil, errors.New("cannot determine config file type from " + tmpFileName)
	}

	helper := &ConfigurationHelper{
		configFileName: tmpFileName,
		extension:      ext,
		loaded:         false,
		configItems:    make(map[string][]configItem),
	}
	configurationHelpers[tmpFileName] = helper
	return helper, nil
}

func (c *ConfigurationHelper) GetConfigurationFileName() string {
	return c.configFileName
}

func (c *ConfigurationHelper) Extension() string {
	return c.extension
}

func (c *ConfigurationHelper) LoadConfigFile(filename string) error {
	//TODO implement me
	helper, err := GetConfigurationWrapper(filename)

	if err != nil {
		return err
	}

	if helper != c {
		return errors.New("not the same configuration file: " + filename)
	}

	return nil
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

// ============================ private functions & methods ==================================

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
