package agentosinterceptor

import (
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

var authInterceptor = common.GetSingleton(func() *AuthenticationInterceptor {
	return &AuthenticationInterceptor{
		BaseInterceptor: basic.BaseInterceptor{
			Order:           1,
			InterceptorName: "authentication_interceptor",
		},
	}
}).Instance()

func GetAuthenticationInterceptor() *AuthenticationInterceptor {
	return authInterceptor
}
