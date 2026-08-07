package dataobject

import "encoding/json"

// LoginSession is the minimal, non-sensitive identity stored in the session cache.
// Passwords, password hashes and the token itself must never be serialized into it.
type LoginSession struct {
	LoginName        string `json:"loginName"`
	DisplayName      string `json:"displayName"`
	IssuedAt         int64  `json:"issuedAt"`
	LastActiveAt     int64  `json:"lastActiveAt"`
	AbsoluteExpireAt int64  `json:"absoluteExpireAt"`
}

func (loginSession *LoginSession) ToJsonString() (string, error) {
	data, err := json.Marshal(loginSession)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func (loginSession *LoginSession) FromJsonString(value string) error {
	return json.Unmarshal([]byte(value), loginSession)
}
