package dataobject

import "encoding/json"

// T_Account user for login
type T_Account struct {
	LoginName   string `json:"login_name"`
	LoginPass   string `json:"login_pass"`
	DisplayName string `json:"display_name"`
	Disabled    bool   `json:"disabled"`

	Token string `json:"token"` //登录成功后返回的token信息
}

func (account *T_Account) ToJsonString() string {
	jsonString, err := json.MarshalIndent(account, "", "  ")
	if err != nil {
		return ""
	}
	return string(jsonString)
}

func (account *T_Account) FromJsonString(jsonStr string) error {
	err := json.Unmarshal([]byte(jsonStr), account)
	if err != nil {
		return err
	}
	return nil
}
