package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
	cclawcommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/request"
	"my.favorite/platformutility/webbase/middleware/basic"
)

func GetAuthentication(c *gin.Context) (*request.LoginSession, bool) {
	tempValue, exists := c.Get(cclawcommon.ContextAuthenticatedUser)
	if !exists {
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusUnauthorized, cclawcommon.ConstErrorUserUnAuthorized, cclawcommon.ConstErrorUserUnAuthorizedMsg, nil)
		return nil, false
	}
	loginSession, ok := tempValue.(*request.LoginSession)
	if !ok {
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
		return nil, false
	}

	return loginSession, true
}
