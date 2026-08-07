package model

import (
	"strings"

	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

// ================ login request and response model ================

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	UserName      string `json:"userName"`
	DisplayName   string `json:"displayName"`
	Token         string `json:"token"`         // 用户登录成功后得到的token
	GenAt         int64  `json:"genAt"`         //token产生的时间
	ExpiredLength int64  `json:"expiredLength"` //token失效时长,unit = second
}

type LoginResponseEx struct {
	basic.CommonResponse
	Data LoginResponse `json:"data"`
}

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
