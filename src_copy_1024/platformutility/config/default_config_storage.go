package config

import (
	"strings"
	"sync"

	"my.favorite/platformutility/exception"
)

type DefaultConfigStore struct {
	mutex sync.RWMutex

	data map[string]map[string]string //存储配置信息
}

func (s *DefaultConfigStore) Get(section string, key string) (string, error) {
	return s.GetWithDefault(section, key, "")
}

func (s *DefaultConfigStore) GetWithDefault(section string, key string, defaultValue string) (string, error) {
	section = strings.TrimSpace(section)
	key = strings.TrimSpace(key)
	if len(section) == 0 || len(key) == 0 {
		return "", exception.ParameterEmptyErrorEx("section or key must not be empty")
	}

	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.getValueWithDefault(section, key, defaultValue)
}

func (s *DefaultConfigStore) Set(section string, key string, newValue string) error {
	//TODO implement me
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if _, ok := s.data[section]; !ok {
		s.data[section] = make(map[string]string)
	}

	s.data[section][key] = newValue
	return nil
}

func (s *DefaultConfigStore) DeleteKey(section string, key string) {
	//TODO implement me
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if _, ok := s.data[section]; !ok {
		delete(s.data, key)
	}
}

func (s *DefaultConfigStore) DeleteSection(section string) {
	//TODO implement me
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.data, section)
}

func (s *DefaultConfigStore) Destroy() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.data = nil
}

// ===================== private functions & methods ===========================

func (s *DefaultConfigStore) getValueWithDefault(section string, key string, defaultValue string) (string, error) {
	if sec, ok := s.data[section]; ok {
		if val, ok := sec[key]; ok {
			return val, nil
		}
	}

	return defaultValue, nil
}
