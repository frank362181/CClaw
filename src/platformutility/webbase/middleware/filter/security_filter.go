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
	"my.favorite/platformutility/common/hash"
	"my.favorite/platformutility/webbase"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type SecurityFilter struct {
	basic.BaseFilter

	deSecurity []string //不需要做安全检查的URL
}

func (sf *SecurityFilter) SetupSecurityFilter(urls []string) {
	for _, url := range urls {
		sf.deSecurity = append(sf.deSecurity, url)
	}
}

func (sf *SecurityFilter) DoFilter() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestInfo := basic.GetBaseRequester().GetOrCreateRequestInfo(c)
		// whether security checking or not
		if sf.needSecurity(requestInfo.RequestPath) {
			// 1 replay attach
			if sf.replayAttackNonce(requestInfo) {
				basic.GetBaseResponser().AbortWithError(c, http.StatusForbidden, basic.ConstNonceInvalid, basic.ConstNonceInvalidMsg, nil)
				return
			}

			//1 replay attack
			if sf.replayAttackTimestamp(requestInfo.QueryTimestamp) {
				basic.GetBaseResponser().AbortWithError(c, http.StatusForbidden, basic.ConstTimeGapOverflow, basic.ConstTimeGapOverflowMsg, nil)
				return
			}

			// 3. 对内容进行signature判断
			//if sf.antiTampering(requestInfo, c) {
			//	basic.GetBaseResponser().AbortWithError(c, http.StatusForbidden, basic.ConstSignatureUnMatched, basic.ConstSignatureUnMatchMsg)
			//	return
			//}
		}

		// 放行
		c.Next()
	}
}

// ================================= private functions & methods ====================================

func (sf *SecurityFilter) needSecurity(url string) bool {
	url = strings.ToLower(url)
	for _, v := range sf.deSecurity {
		tempUrl := webbase.BasePath + "/" + v
		if strings.EqualFold(url, tempUrl) {
			//不需要安全检查
			return false
		}
	}

	return true
}

/*
* 判断客户端请求的时间与服务端是否超过指定的阈值
 */
func (sf *SecurityFilter) replayAttackTimestamp(originalTimestamp int64) bool {
	currentNow := common.GetDatetimeUtilityInstance().GetCurrentDatetime()
	if currentNow.Timestamp-originalTimestamp > basic.ConstClientServerGap {
		return true
	}

	return false
}

func (sf *SecurityFilter) replayAttackNonce(r *basic.RequestInfo) bool {
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
	tempStr1 := strconv.FormatInt(r.QueryTimestamp, 10)
	tempStr2 := r.Nonce

	signature := hash.MD5(bodyString + tempStr1 + tempStr2)
	if strings.EqualFold(signature, r.Signature) {
		//此时要放行
		return false
	}

	return true
}
