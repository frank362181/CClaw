package config

import (
	"CClaw/exception"
	"strings"
)

func (s *ConfigurationStore) Get(section string, key string) (string, error) {
	defaultValue := ""
	return s.GetWithDefault(section, key, defaultValue)
}

func (s *ConfigurationStore) GetWithDefault(section string, key string, defaultValue string) (string, error) {
	section = strings.TrimSpace(section)
	key = strings.TrimSpace(key)
	if len(section) == 0 || len(key) == 0 {
		return "", exception.ParameterEmptyErrorEx("section or key must not be empty")
	}

	s.mutex.RLock()
	s.mutex.RUnlock()
	return s.getValueWithDefault(section, key, defaultValue)
}

func (s *ConfigurationStore) Set(section string, key string, newValue string) error {
	//TODO implement me
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if _, ok := s.data[section]; !ok {
		s.data[section] = make(map[string]string)
	}

	s.data[section][key] = newValue
	return nil
}

func (s *ConfigurationStore) DeleteKey(section string, key string) {
	//TODO implement me
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if _, ok := s.data[section]; !ok {
		delete(s.data, key)
	}
}

func (s *ConfigurationStore) DeleteSection(section string) {
	//TODO implement me
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.data, section)
}

// ===================== private functions & methods ===========================

func (s *ConfigurationStore) getValueWithDefault(section string, key string, defaultValue string) (string, error) {
	if sec, ok := s.data[section]; ok {
		return sec[key], nil
	}

	return defaultValue, nil
}
