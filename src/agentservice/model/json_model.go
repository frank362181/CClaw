package model

type LoginRequestModel struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponseModel struct {
	Username      string `json:"username"`
	Token         string `json:"token"`          // 用户登录成功后得到的token
	GenAt         int64  `json:"gen_at"`         //token产生的时间
	ExpiredLength int64  `json:"expired_length"` //token失效时长,unit = second
}
