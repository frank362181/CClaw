package model

import "my.favorite/platformutility/webbase/middleware/basic"

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
