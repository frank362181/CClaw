package dataobject

import "encoding/json"

// TAccount user for login
type TAccount struct {
	ComCode         string `json:"comCode"`
	LoginName       string `json:"loginName"`
	LoginKey        string `json:"-"`
	LoginPass       string `json:"-"`
	AccountCategory int    `json:"accountCategory"`
	DisplayName     string `json:"displayName"`
	IdName          string `json:"IdName"`
	ImgUrl          string `json:"imgUrl"`
	Mobile          string `json:"mobile"`
	Email           string `json:"email"`
	Trail           int    `json:"trail"`
	GenDate         int64  `json:"genDate"`
	GraceDate       int64  `json:"graceDate"`
	ExpDate         int64  `json:"expDate"`
	Expired         int    `json:"expired"`
	Disabled        bool   `json:"disabled"`
	Deleted         bool   `json:"deleted"`
	Mark            string `json:"mark"`
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
