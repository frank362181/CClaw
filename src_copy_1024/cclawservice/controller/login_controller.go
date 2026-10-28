package controller

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	exception2 "my.favorite/cclawservice/exception"
	model "my.favorite/cclawservice/request"
	"my.favorite/cclawservice/service"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
	"my.favorite/platformutility/webbase/middleware/controller"
	baseController "my.favorite/platformutility/webbase/middleware/controller"
	pService "my.favorite/platformutility/webbase/middleware/service"
)

type LoginController struct {
	baseController.BaseController
}

var loginController = common.GetSingleton(func() *LoginController {
	return &LoginController{
		BaseController: controller.BaseController{
			Type:    basic.APIGroupRequire,
			Name:    "loggingController",
			Service: nil,
		},
	}
}).Instance()

func GetLoginController(service pService.IService) *LoginController {
	loginController.Service = service
	return loginController
}

// Login (用户登入)
//
//	@Summary		用户登入API
//	@Description	实现用户登录功能
//	@Tags			用户登入登出
//	@Accept			json
//	@Produce		json
//	@Param			NONCE		query		string				true	"盐"
//	@Param			TIMESTAMP	query		string				true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE	query		string				true	"对请求内容的签名"
//	@Param			body		body		model.LoginRequest	true	"登录用户名和密码"
//	@Success		200			{object}	basic.CommonResponse
//	@Failure		400			{object}	basic.CommonResponse
//	@Router			/api/login [post]
func (lc *LoginController) Login(c *gin.Context) {
	loginService, ok := lc.getLoginService(c)
	if !ok {
		return
	}

	var lq model.LoginRequest
	err := c.ShouldBindBodyWithJSON(&lq) //这里将body读取之后,后面如果想要再次读取将会失败
	if err != nil {
		respondKind(c, exception2.LoginParameterKind)
		return
	}

	loginSession, err1 := loginService.Login(lq.Username, lq.Password)
	if err1 != nil {
		respondLoginError(c, err1)
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, loginSession)
}

// Logout (用户登出)
//
//	@Summary		用户登出API
//	@Description	实现用户登出功能
//	@Tags			用户登入登出
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string	true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string	true	"盐"
//	@Param			TIMESTAMP		query		string	true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string	true	"对请求内容的签名秒数"
//	@Success		200				{object}	basic.CommonResponse
//	@Failure		400				{object}	basic.CommonResponse
//	@Router			/api/logout [get]
//	@Router			/api/logout [post]
func (lc *LoginController) Logout(c *gin.Context) {
	loginService, ok := lc.getLoginService(c)
	if !ok {
		return
	}

	token := strings.TrimSpace(c.GetHeader(basic.ConstHeaderToken))
	if err := loginService.Logout(token); err != nil {
		respondLoginError(c, err)
		return
	}
	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, nil)
}

// ================== private functions & methods ========================

func (lc *LoginController) getLoginService(c *gin.Context) (*service.LoginService, bool) {
	ls, ok := lc.GetService().(*service.LoginService)
	if !ok || ls == nil {
		//服务器内部错误
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
		return nil, false
	}

	return ls, true
}
