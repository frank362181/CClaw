package contnroller

import (
	"net/http"

	"github.com/gin-gonic/gin"
	agentoscommon "my.favorite/agentos/common"
	"my.favorite/agentos/model"
	"my.favorite/agentos/service"
	"my.favorite/platformutility/common"
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
// @Success      200  {object} basic.CommonResponseMsg
// @Failure      400  {object} basic.CommonResponseMsg
// @Router       /login [post]
func (lc *LoginController) Login(c *gin.Context) {
	ls := lc.GetService().(*service.LoginService)
	if ls == nil {
		basic.GetBaseResponser().WriteWithError(c, http.StatusOK, agentoscommon.ConstErrorLoginParameter, agentoscommon.ConstErrorLoginParameterMsg, nil)
		return
	}

	var lq model.LoginRequestModel
	err := c.ShouldBindBodyWithJSON(&lq)
	if err != nil {
		basic.GetBaseResponser().WriteWithError(c, http.StatusOK, agentoscommon.ConstErrorLoginParameter, agentoscommon.ConstErrorLoginParameterMsg, err.Error())
		return
	}

	err = ls.Login(&lq)
	if err != nil {
		basic.GetBaseResponser().WriteWithError(c, http.StatusOK, agentoscommon.ConstErrorLoginParameter, agentoscommon.ConstErrorLoginParameterMsg, err.Error())
		return
	}

	token, _ := hash.SHA256(lq.Username + lq.Password + common.GetDatetimeUtilityInstance().GetCurrentDatetime().DatetimeStr) //根据username + password + 当前日期
	resp := model.GenerationLoginResponse(lq.Username, token)
	basic.GetBaseResponser().Success(c, http.StatusOK, agentoscommon.ConstErrorNone, agentoscommon.ConstErrorNoneMsg, resp)
}

// Logout (用户登出)
// @Summary      用户登出API
// @Description  实现用户登出功能
// @Tags         用户登入登出
// @Accept       json
// @Produce      json
// @Param		 AUTHORIZATION header string true "Login成功返回后得到的token"
// @Param        Nonce query string true "盐"
// @Param		 Timestamp query string true "数值,表示请求端请求的时间(秒数)"
// @Param		 Signature query string true "对请求内容的签名秒数"
// @Success      200  {object} basic.CommonResponseMsg
// @Failure      400  {object} basic.CommonResponseMsg
// @Router       /logout [get] [post]
func (lc *LoginController) Logout(c *gin.Context) {
	return
}
