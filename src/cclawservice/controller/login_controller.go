package controller

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	cclawcommon "my.favorite/cclawservice/common"
	model2 "my.favorite/cclawservice/controller/model"
	exception2 "my.favorite/cclawservice/exception"
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

func NewLoginController(service pService.IService) *LoginController {
	loginController := &LoginController{
		BaseController: controller.BaseController{
			Type:    basic.APIGroupRequire,
			Name:    "loggingController",
			Service: nil,
		},
	}

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
//	@Success		200			{object}	model.LoginResponseEx
//	@Failure		400			{object}	basic.CommonResponse
//	@Router			/api/login [post]
func (lc *LoginController) Login(c *gin.Context) {
	loginService, ok := lc.GetService().(*service.LoginService)
	if !ok || loginService == nil {
		//内部服务器错误
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
		return
	}

	var lq model2.LoginRequest
	err := c.ShouldBindBodyWithJSON(&lq) //这里将body读取之后,后面如果想要再次读取将会失败
	if err != nil {
		basic.GetBaseResponser().Failure(c, http.StatusBadRequest, cclawcommon.ConstErrorLoginParameter, cclawcommon.ConstErrorLoginParameterMsg, nil)
		return
	}

	loginSession, err1 := loginService.Login(lq.Username, lq.Password)
	if err1 != nil {
		if errors.Is(err1, exception2.UserPasswordUnmatchErr) {
			basic.GetBaseResponser().Failure(c, http.StatusOK, cclawcommon.ConstErrorUserNamePasswordUnmatch, cclawcommon.ConstErrorUserNamePasswordUnmatchMsg, nil)
			return
		}
		basic.GetBaseResponser().Failure(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
		return
	}

	//resp := model2.GenerationLoginResponse(user)
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
	token := strings.TrimSpace(c.GetHeader(basic.ConstHeaderToken))
	loginService, ok := lc.GetService().(*service.LoginService)
	if !ok || loginService == nil {
		//内部服务器错误
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
		return
	}

	if err := loginService.Logout(token); err != nil {
		basic.GetBaseResponser().Failure(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
		return
	}
	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, nil)
}
