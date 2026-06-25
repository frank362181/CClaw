/*
* 安全 filter
 */

package filter

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unsafe"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type SecurityFilter struct {
	basic.BaseFilter
}

func (sf *SecurityFilter) DoFilter() gin.HandlerFunc {
	return func(c *gin.Context) {

		requestInfo := basic.GetBaseRequester().GetOrCreateRequestInfo(c)

		// 1 replay attack
		if sf.replayAttack1(requestInfo) {
			basic.GetBaseResponser().AbortWithError(c, http.StatusForbidden, basic.ConstGapGreaterStd, basic.ConstGapGreatStdMsg)
			return
		}

		// 2 replay attach
		if sf.replayAttack2(requestInfo) {
			basic.GetBaseResponser().AbortWithError(c, http.StatusForbidden, basic.ConstNonceInvalid, basic.ConstNonceInvalidMsg)
			return
		}

		// 3. 对内容进行signature判断
		if sf.antiTampering(requestInfo, c) {
			basic.GetBaseResponser().AbortWithError(c, http.StatusForbidden, basic.ConstSignatureUnMatched, basic.ConstSignatureUnMatchMsg)
			return
		}

		// 放行
		c.Next()
	}
}

// ================================= private functions & methods ====================================

/*
* 判断客户端请求的时间与服务端是否超过指定的阈值
 */
func (sf *SecurityFilter) replayAttack1(requestInfo *basic.RequestInfo) bool {
	currentNow := common.GetDatetimeUtilityInstance().GetCurrentDatetime()
	if currentNow.Timestamp-requestInfo.QueryStringTimestamp > basic.ConstClientServerGap {
		return true
	}

	return false
}

func (sf *SecurityFilter) replayAttack2(r *basic.RequestInfo) bool {
	//判断是否有nonce
	tempNonce := strings.TrimSpace(r.Nonce)
	if len(tempNonce) == 0 {
		return true
	}

	//判断过去 5 分钟nonce是否曾经使用过，结合redis

	return false
}

func (sf *SecurityFilter) antiTampering(r *basic.RequestInfo, c *gin.Context) bool {
	bodyBytes, err := io.ReadAll(c.Request.Body)
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes)) // 恢复body供后续调用

	if err != nil {
		return true
	}

	bodyString := *(*string)(unsafe.Pointer(&bodyBytes)) //要确保bodyBytes数据不会被修改
	tempStr1 := strconv.FormatInt(r.QueryStringTimestamp, 10)
	tempStr2 := r.Nonce

	signature := common.GetCryptoInstance().MD5(bodyString + tempStr1 + tempStr2)
	if strings.EqualFold(signature, r.Signature) {
		//此时要放行
		return false
	}

	return true
}
