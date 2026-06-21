package config

import (
	"CClaw/exception"
	"os"
	"strings"

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
		if strings.EqualFold(section.Name(), ini.DefaultSection) && len(section.Keys()) == 0 {
			continue
		}

		sectionName := section.Name()
		if _, exists := store.data[sectionName]; !exists {
			store.data[sectionName] = make(map[string]string)
		}

		for _, key := range section.Keys() {
			store.data[sectionName][key.Name()] = key.String()
		}
	}

	return store, nil
}

func (i *IniConfigParser) Save(filename string, store ConfigurationStore) error {
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
