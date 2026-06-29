package filter

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/logging"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type GlobalRecoveryFilter struct {
	basic.BaseFilter
}

func (cf *GlobalRecoveryFilter) DoFilter() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				// 获取堆栈上的信息 并打印日志
				stackString := string(debug.Stack())
				logging.LogError(stackString)

				// 如果作为filter列表的一份子已经abort，则可不需要执行
				if !c.IsAborted() {
					basic.GetBaseResponser().AbortWithError(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
				}

				// defer执行完成后调用链会继续返回
			}
		}()

		basic.GetBaseRequester().GetOrCreateRequestInfo(c)
		c.Next()
	}
}
