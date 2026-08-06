package model

import (
	"strings"

	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

func GenerationLoginRequest(username string, password string) *LoginRequest {
	username = strings.TrimSpace(username)
	if len(username) == 0 {
		return nil
	}

	password = strings.TrimSpace(password)
	username = strings.ToLower(username) //不区分大小写

	loginRequest := &LoginRequest{
		Username: username,
		Password: password,
	}

	return loginRequest
}

func GenerationLoginResponse(user *dataobject.TAccount) *LoginResponse {
	if user == nil {
		return nil
	}

	loginResponse := &LoginResponse{
		UserName:      user.LoginName,
		DisplayName:   user.DisplayName,
		Token:         user.Token,
		GenAt:         common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp,
		ExpiredLength: basic.MemoryTokenExpireTime, //default expired length, 1800 seconds
	}

	return loginResponse
}
