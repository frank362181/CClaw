package config

import "my.favorite/platformutility/common"

type JsonConfigParser struct{}

func (j *JsonConfigParser) Load(fileName string) (IConfigurationStorage, error) {
	jsonData, err := common.ReadFileAll(fileName)
	if err != nil {
		return nil, err
	}

	store := &JsonConfigStore{}
	store.data = string(jsonData)

	return store, nil
}

func (j *JsonConfigParser) Save(fileName string, storage IConfigurationStorage) error {
	return nil
}
