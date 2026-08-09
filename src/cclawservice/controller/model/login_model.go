package model

import (
	"encoding/json"
)

// ================ login request and response model ================

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginSession is the minimal, non-sensitive identity stored in the session cache.
// Passwords, password hashes and the token itself must never be serialized into it.
type LoginSession struct {
	ComCode          string `json:"comCode"`
	LoginName        string `json:"loginName"`
	DisplayName      string `json:"displayName"`
	IssuedAt         int64  `json:"issuedAt"` // timestamp for generating the token
	LastActiveAt     int64  `json:"lastActiveAt"`
	AbsoluteExpireAt int64  `json:"absoluteExpireAt"`

	Token         string `json:"token"`
	ExpiredLength int64  `json:"expiredLength"` //token expired length in seconds
}

func NewLoginSession(comCode string, loginName string, displayName string) *LoginSession {
	return &LoginSession{
		ComCode:     comCode,
		LoginName:   loginName,
		DisplayName: displayName,
	}
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
