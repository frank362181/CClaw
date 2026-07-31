package interceptor

import (
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
	"my.favorite/platformutility/webbase/middleware/service"

	memoryCommon "my.favorite/platformutility/memory_db/common"
)

var authInterceptor = common.GetSingleton(func() *AuthenticationInterceptor {
	return &AuthenticationInterceptor{
		BaseInterceptor: basic.BaseInterceptor{
			Order:           1,
			InterceptorName: "authentication_interceptor",
		},
		userService: nil, // initializes nil
	}
}).Instance()

func GetAuthenticationInterceptor(memoryDb memoryCommon.IMemoryDB, svc service.IService) *AuthenticationInterceptor {
	authInterceptor.memoryDb = memoryDb
	authInterceptor.userService = svc
	//authInterceptor.SetService(svc) //设置业务处理的逻辑
	return authInterceptor
}
