package config

import (
	"path/filepath"
	"strings"
	"sync"

	"my.favorite/platformutility/exception"
)

var parsers = map[string]IConfigurationParser{
	//载入不同配置文件的配置类
	FileIni:        &IniConfigParser{},
	FileProperties: &PropertiesConfigParser{},
	FileYaml:       &YamlConfigParser{},
	FileJson:       &JsonConfigParser{},
}

// configurationHelper 与配置文件类型关联
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
	if len(filename) == 0 {
		return nil, exception.ParameterEmptyErrorEx("filename must not be empty")
	}

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

	// 先探测是否已存在(只关心 err,helper 本身不参与后续逻辑)
	if _, err := findHelper(filename); err != nil {
		return nil, err
	}

	return addHelper(filename)
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

func GetParser(ext string) (IConfigurationParser, error) {
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
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	if ext == "" {
		return nil, exception.FileExtensionErrorEx("cannot determine config file type from " + filename)
	}

	helper := &ConfigurationHelper{
		configFileName: filename,
		extension:      ext,
		loaded:         false,
		storage:        nil,
	}
	configurationHelpers[filename] = helper
	return helper, nil
}
