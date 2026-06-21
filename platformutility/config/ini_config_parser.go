package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/exception"

	"github.com/go-ini/ini"
)

type IniConfigParser struct{}

// Load :load the ini configuration file and parse it
func (i *IniConfigParser) Load(filename string) (*ConfigurationStore, error) {
	//TODO implement me
	iniFile, err := ini.LoadSources(ini.LoadOptions{
		Insensitive:                false,
		AllowPythonMultilineValues: true,
	}, filename)

	if err != nil {
		return nil, exception.UnloadFileErrorEx("unload the config filename:" + filename)
	}

	// 设置环境变量展开
	iniFile.ValueMapper = func(raw string) string {
		// 先使用 os.ExpandEnv 处理 ${VAR} 形式
		expanded := os.ExpandEnv(raw)

		// 再手动处理 %VAR% 形式
		expanded = expandPercentVars(expanded)
		return expanded
	}

	store := &ConfigurationStore{
		data: make(map[string]map[string]string),
	}

	for _, section := range iniFile.Sections() {
		// 跳过默认的 DEFAULT section (ini自带的)
		if strings.EqualFold(section.Name(), ini.DefaultSection) || len(section.Keys()) == 0 {
			continue
		}

		sectionName := section.Name()
		if _, exists := store.data[sectionName]; !exists {
			store.data[sectionName] = make(map[string]string)
		}

		for _, key := range section.Keys() {
			store.data[sectionName][key.Name()] = key.Value()
		}
	}

	return store, nil
}

func (i *IniConfigParser) Save(filename string, store *ConfigurationStore) error {
	//TODO implement me
	filename = strings.TrimSpace(filename)
	if len(filename) == 0 {
		return exception.ParameterEmptyErrorEx("parameter must not be empty")
	}

	if store.data == nil || len(store.data) == 0 {
		return nil
	}

	// store the config item into file
	store.mutex.RLock()
	defer store.mutex.RUnlock()

	//make sure the exists of the DIR
	dir := filepath.Dir(filename)
	err := common.GetOSUtilityInstance().CreateDir(dir)
	if err != nil {
		return exception.FilePathErrorEx("can not create dir:" + dir)
	}

	tmpFile := dir + ".tmp" + fmt.Sprintf("%d", common.GetOSUtilityInstance().GetProcessID())
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return exception.FilePathErrorEx("can not open file:" + tmpFile)
	}

	writeErr := i.writeToWriter(f, store)
	closeErr := f.Close()

	if writeErr != nil {
		os.Remove(tmpFile)
		return writeErr
	}

	if closeErr != nil {
		os.Remove(tmpFile)
		return closeErr
	}

	//原子替换
	if err := os.Rename(tmpFile, filename); err != nil {
		os.Remove(tmpFile)
		return exception.FilePathErrorEx("can not rename file:" + tmpFile)
	}

	return nil
}

// ================================ private functions & methods =====================================
func expandPercentVars(s string) string {
	//简单实现,找到 %XXX% 并替换为 os.Getenv("XX")
	for {
		start := strings.Index(s, "%")
		if start == -1 {
			continue
		}

		end := strings.Index(s[start+1:], "%")
		if end == -1 {
			break
		}

		end += start + 1
		varName := s[start+1 : end]
		replacement := os.Getenv(varName)
		s = s[:start] + replacement + s[end+1:]
	}

	return s
}

func (i *IniConfigParser) writeToWriter(w io.Writer, store *ConfigurationStore) error {
	iniFile := ini.Empty()

	// 按照section写入
	for sectionName, keys := range store.data {
		section, err := iniFile.NewSection(sectionName)
		if err != nil {
			return exception.ConfigErrorEx("can not create section:" + sectionName)
		}

		for key, value := range keys {
			_, err := section.NewKey(key, value)
			if err != nil {
				return exception.ConfigErrorEx("can not create key:" + key)
			}

			// 如果有注释
		}
	}

	_, err := iniFile.WriteTo(w)
	return err
}
