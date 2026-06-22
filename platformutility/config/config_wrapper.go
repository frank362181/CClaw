package config

import (
	"strings"
	"sync"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/exception"
)

var parsers = map[string]ConfigurationParser{
	//载入不同配置文件的配置类
	"ini":        &IniConfigParser{},
	"properties": &PropertiesConfigParser{},
	"yaml":       &YamlConfigParser{},
}

var (
	mutex                sync.RWMutex
	configurationHelpers = make(map[string]*ConfigurationHelper)
)

// GetConfigurationHelper singleton,应用级别
func GetConfigurationHelper(filename string) (*ConfigurationHelper, error) {
	filename = strings.TrimSpace(filename)
	if len(filename) == 0 {
		return nil, exception.ParameterEmptyErrorEx("filename must not be empty")
	}

	mutex.RLock()
	if helper, ok := configurationHelpers[filename]; ok {
		mutex.RUnlock()
		return helper, nil
	}
	mutex.RUnlock()

	// 不存在 create a new
	mutex.Lock()
	defer mutex.Unlock()

	//double check
	if helper, ok := configurationHelpers[filename]; ok {
		return helper, nil
	}

	// add a new helper
	helper, err := addHelper(filename)
	if err == nil {
		return helper, nil
	}

	return nil, exception.UnknownErrorEx("unknown error happened: " + filename)
}

func LookupConfigurationHelper(filename string) (*ConfigurationHelper, error) {
	filename = strings.TrimSpace(filename)
	mutex.RLock()
	defer mutex.RUnlock()

	return findHelper(filename)
}

func InsertConfigurationHelper(filename string) (*ConfigurationHelper, error) {
	filename = strings.TrimSpace(filename)
	if len(filename) == 0 {
		return nil, exception.ParameterEmptyErrorEx("filename must not be empty")
	}

	mutex.Lock()
	defer mutex.Unlock()

	helper, err := findHelper(filename)
	if err != nil {
		return nil, err
	}

	helper, err = addHelper(filename)
	return helper, err
}

func InsertConfigurationHelperEx(helper *ConfigurationHelper) bool {
	if helper == nil {
		return false
	}

	mutex.Lock()
	defer mutex.Unlock()

	_, err := findHelper(helper.configFileName)
	if err != nil {
		return false
	}

	configurationHelpers[helper.configFileName] = helper
	return true
}

func GetParser(ext string) (ConfigurationParser, error) {
	if p, ok := parsers[ext]; ok {
		return p, nil
	}
	return nil, exception.UnknownErrorEx("config file unsupported: " + ext)
}

// ================================= private functions & methods =============================

func findHelper(filename string) (*ConfigurationHelper, error) {
	if helper, ok := configurationHelpers[filename]; ok {
		return helper, nil
	}

	return nil, exception.UnMatchErrorEx("configuration not found: " + filename)
}

func addHelper(filename string) (*ConfigurationHelper, error) {
	ext := common.GetOSUtilityInstance().GetExtension(filename)
	if ext == "" {
		return nil, exception.FileExtensionErrorEx("cannot determine config file type from " + filename)
	}

	helper := &ConfigurationHelper{
		configFileName: filename,
		extension:      ext,
		loaded:         false,
		storage:        &configurationStore{data: make(map[string]map[string]string)},
	}
	configurationHelpers[filename] = helper
	return helper, nil
}
