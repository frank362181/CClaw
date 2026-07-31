package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
	cclawcommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/model"
	"my.favorite/cclawservice/service"
	platformcommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/common/hash"
	"my.favorite/platformutility/webbase/middleware/basic"

	"my.favorite/platformutility/webbase/middleware/controller"
)

type LoginController struct {
	controller.BaseController
}

// Login (用户登入)
// @Summary      用户登入API
// @Description  实现用户登录功能
// @Tags         用户登入登出
// @Accept       json
// @Produce      json
// @Param		 TIMESTAMP header string true "数值,表示请求端请求的时间(秒数)"
// @Param		 SIGNATURE header string true "对请求内容的签名秒数"
// @Param        NONCE query string true "盐"
// @Param		 TIMESTAMP query string true "数值,表示请求端请求的时间(秒数)"
// @Param		 SIGNATURE query string true "对请求内容的签名秒数"
// @Param		 body      body  model.LoginRequestModel true "登录用户名和密码"
// @Success      200  {object} basic.CommonResponseMsg
// @Failure      400  {object} basic.CommonResponseMsg
// @Router       /login [post]
func (lc *LoginController) Login(c *gin.Context) {
	us := lc.GetService().(*service.UserService)
	if us == nil {
		//内部服务器错误
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusOK, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
		return
	}

	var lq model.LoginRequestModel
	err := c.ShouldBindBodyWithJSON(&lq) //这里将body读取之后,后面如果想要再次读取将会失败
	if err != nil {
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusOK, cclawcommon.ConstErrorLoginParameter, cclawcommon.ConstErrorLoginParameterMsg, err.Error())
		return
	}

	user, err1 := us.Login(lq.Username, lq.Password)
	if err1 != nil {
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusOK, cclawcommon.ConstErrorLoginParameter, cclawcommon.ConstErrorLoginParameterMsg, err1.Error())
		return
	}

	token, _ := hash.SHA256(user.LoginName + user.LoginPass + platformcommon.GetDatetimeUtilityInstance().GetCurrentDatetime().DatetimeStr) //根据username + password + 当前日期
	resp := model.GenerationLoginResponse(user.LoginName, token)
	basic.GetBaseResponser().Success(c, http.StatusOK, cclawcommon.ConstErrorNone, cclawcommon.ConstErrorNoneMsg, resp)
}

// Logout (用户登出)
// @Summary      用户登出API
// @Description  实现用户登出功能
// @Tags         用户登入登出
// @Accept       json
// @Produce      json
// @Param		 AUTHORIZATION header string true "Login成功返回后得到的token"
// @Param        NONCE query string true "盐"
// @Param		 TIMESTAMP query string true "数值,表示请求端请求的时间(秒数)"
// @Param		 SIGNATURE query string true "对请求内容的签名秒数"
// @Success      200  {object} basic.CommonResponseMsg
// @Failure      400  {object} basic.CommonResponseMsg
// @Router       /logout [get] [post]
func (lc *LoginController) Logout(c *gin.Context) {
	us := lc.GetService().(*service.UserService)
	if us == nil {
		//内部服务器错误
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusOK, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
		return
	}

	err := us.Logout()
	if err != nil {
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, nil)
}
