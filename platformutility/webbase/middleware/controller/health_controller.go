package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type HealthController struct {
	BaseController
}

// OK (健康检测函数)
// @Summary      健康检测函数
// @Description  返回健康检测结果，JSON
// @Tags         健康检测
// @Accept       json
// @Produce      json
// @Success      200  {object}  basic.CommonResponseMsg
// @Failure 	 400  {object}  basic.CommonResponseMsg
// @Router       /heartbeat [get]
func (hc *HealthController) OK(c *gin.Context) {
	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, nil)
}
