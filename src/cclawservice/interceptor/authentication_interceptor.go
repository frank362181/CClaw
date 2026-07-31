/*
* 是否需要用户校验的interceptor
 */

package interceptor

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"my.favorite/cclawservice/common"
	"my.favorite/cclawservice/model/dataobject"
	memoryCommon "my.favorite/platformutility/memory_db/common"
	"my.favorite/platformutility/webbase"
	"my.favorite/platformutility/webbase/middleware/basic"
	"my.favorite/platformutility/webbase/middleware/service"
)

type AuthenticationInterceptor struct {
	basic.BaseInterceptor

	UnWantedTokenUrls []string //不需要token的URL列表

	// 业务处理的Service
	userService service.IService

	//内存数据库
	memoryDb memoryCommon.IMemoryDB
}

func (i *AuthenticationInterceptor) SetupUnwantedTokenUrls(urls []string) {
	for _, url := range urls {
		i.UnWantedTokenUrls = append(i.UnWantedTokenUrls, url)
	}
}

func (i *AuthenticationInterceptor) GetService() service.IService {
	return i.userService
}

func (i *AuthenticationInterceptor) PreHandle(c *gin.Context) bool {
	reqInfo := basic.GetBaseRequester().GetOrCreateRequestInfo(c)

	if i.unwantedTokenUrl(reqInfo.RequestPath) {
		return true
	}

	//此时需要先判断token的情况,为空、过期、不存在等
	token := c.GetHeader(basic.ConstHeaderToken)
	if len(token) == 0 {
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusOK, common.ConstErrorNonToken, common.ConstErrorNonTokenMsg, nil)
		return false
	}

	var userAccount string
	var err error
	if i.memoryDb != nil {
		// 此时要从内存数据库中获取指定的token
		tokenkey := common.MemoryTokenUser + token
		userAccount, err = i.memoryDb.Get(tokenkey)
		if err != nil || len(userAccount) == 0 {
			//无效的token
			basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusOK, common.ConstErrorInvalidToken, common.ConstErrorInvalidTokenMsg, nil)
			return false
		}
	}

	// 此时要从数据库中获取指定的token，此时是i必须要经过登录的API
	userService, ok := i.userService.(interface {
		GetUserByAccount(string) (*dataobject.T_Account, error)
	})
	if !ok {
		//服务器内部出错
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
		return false
	}
	userInfo, err1 := userService.GetUserByAccount(userAccount)
	if err1 != nil || userInfo == nil {
		//此时是无效的token信息
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusOK, common.ConstErrorInvalidToken, common.ConstErrorInvalidTokenMsg, nil)
		return false
	}

	return true
}

func (i *AuthenticationInterceptor) PostHandle(c *gin.Context) {}

func (i *AuthenticationInterceptor) AfterCompletion(c *gin.Context) {

}

// ========================== private functions & methods ==================================

/*
* 对是否是不需要token的request path进行判断
 */
func (i *AuthenticationInterceptor) unwantedTokenUrl(requestPath string) bool {
	for _, url := range i.UnWantedTokenUrls {
		tempStr := webbase.BasePath + "/" + url
		tempStr = strings.ToLower(tempStr)
		if strings.EqualFold(tempStr, requestPath) {
			return true
		}
	}

	return false
}
