package model

import (
	"strings"

	"my.favorite/platformutility/common"
)

func GenerationLoginRequest(username string, password string) interface{} {
	username = strings.TrimSpace(username)
	if len(username) == 0 {
		return nil
	}

	password = strings.TrimSpace(password)
	username = strings.ToLower(username) //不区分大小写

	loginRequest := &LoginRequestModel{
		Username: username,
		Password: password,
	}

	return loginRequest
}

func GenerationLoginResponse(username string, token string) interface{} {
	username = strings.TrimSpace(username)
	if len(username) == 0 {
		return nil
	}

	username = strings.ToLower(username)

	loginResponse := &LoginResponseModel{
		Username:      username,
		Token:         token,
		GenAt:         common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp,
		ExpiredLength: 30 * 60, //default expired length, 1800 seconds
	}

	return loginResponse
}
