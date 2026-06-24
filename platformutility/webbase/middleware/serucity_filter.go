/*
* 安全 filter
 */

package middleware

import (
	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/common"
)

type SecurityFilter struct {
	BaseFilter
}

func (sf *SecurityFilter) DoFilter() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestInfo := sf.getRequestInfo(c)

		// 1 replay attack
		if sf.replayAttack1(requestInfo) {
			return
		}

		// 2 replay attach
		if sf.replayAttack2(requestInfo) {
			return
		}

		// 3. 对内容进行signature判断
		if sf.antiTampering(requestInfo) {
			return
		}

		// 放行
		c.Next()
	}
}

// ================================= private functions & methods ====================================

// ===================== 判断客户端请求的时间与服务端是否超过指定的阈值

func (sf *SecurityFilter) replayAttack1(requestInfo *RequestInfo) bool {
	currentNow := common.GetDatetimeUtilityInstance().GetCurrentDatetime()
	if currentNow.Timestamp-requestInfo.QueryStringTimestamp > ConstClientServerGap {
		return true
	}

	return false
}

func (sf *SecurityFilter) replayAttack2(requestInfo *RequestInfo) bool {
	//判断是否有nonce
	return false
}

func (sf *SecurityFilter) antiTampering(r *RequestInfo) bool {
	return false
}
