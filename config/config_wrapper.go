package config

import (
	"CClaw/common"
	"errors"
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

// GetConfigurationHelper singleton,应用级别
func GetConfigurationHelper(filename string) (*ConfigurationHelper, error) {
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

	// add a new helper
	helper, err := addHelper(filename)
	if err != nil {
		return helper, nil
	}

	return nil, errors.New("can not load the configuration " + filename)
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
		return nil, errors.New("filename must not be empty")
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

// ================================= private functions & methods =============================

func findHelper(filename string) (*ConfigurationHelper, error) {
	if helper, ok := configurationHelpers[filename]; ok {
		return helper, nil
	}

	return nil, errors.New("unfound " + filename)
}

func addHelper(filename string) (*ConfigurationHelper, error) {
	ext := common.GetOSUtilityInstance().GetExtension(filename)
	if ext == "" {
		return nil, errors.New("cannot determine config file type from " + filename)
	}

	helper := &ConfigurationHelper{
		configFileName: filename,
		extension:      ext,
		loaded:         false,
		configItems:    make(map[string][]configItem),
	}
	configurationHelpers[filename] = helper
	return helper, nil
}
