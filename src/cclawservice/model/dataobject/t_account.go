package dataobject

import "encoding/json"

// TAccount user for login
type TAccount struct {
	LoginName   string `json:"login_name"`
	LoginPass   string `json:"-"`
	DisplayName string `json:"display_name"`
	Disabled    bool   `json:"disabled"`

	Token string `gorm:"-" json:"token,omitempty"` //登录成功后返回的token信息
}

func (account *TAccount) ToJsonString() (string, error) {
	jsonString, err := json.Marshal(account)
	if err != nil {
		return "", err
	}
	return string(jsonString), nil
}

func (account *TAccount) FromJsonString(jsonStr string) error {
	err := json.Unmarshal([]byte(jsonStr), account)
	if err != nil {
		return err
	}
	return nil
}
