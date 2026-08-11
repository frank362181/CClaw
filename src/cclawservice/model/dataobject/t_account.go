/**
* 表示用户登录的账户相关的信息，如用户关联的企业等将来都在这里维护
 */

package dataobject

import (
	"encoding/json"

	"gorm.io/plugin/optimisticlock"
)

// TAccount user for login
type TAccount struct {
	ComCode         string `json:"comCode"`
	LoginName       string `json:"loginName"`
	LoginKey        string `json:"-"`
	LoginPass       string `json:"-"`
	AccountCategory uint8  `json:"accountCategory"`
	DisplayName     string `json:"displayName"`
	IdName          string `json:"IdName"`
	ImgUrl          string `json:"imgUrl"`
	Mobile          string `json:"mobile"`
	Email           string `json:"email"`
	Trail           bool   `json:"trail"`
	GenDate         int64  `json:"genDate"`
	GraceDate       int64  `json:"graceDate"`
	ExpDate         int64  `json:"expDate"`
	Expired         bool   `json:"expired"`
	Disabled        bool   `json:"disabled"`
	Deleted         bool   `json:"deleted"`
	Mark            string `json:"mark"`

	DVersion optimisticlock.Version `json:"-"` /*表示数据库中的乐观锁*/
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
