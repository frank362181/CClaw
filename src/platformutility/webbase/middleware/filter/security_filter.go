/*
* 安全 filter
 */

package filter

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/common/hash"
	common2 "my.favorite/platformutility/memory_db/common"
	"my.favorite/platformutility/webbase"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type SecurityFilter struct {
	basic.BaseFilter

	deSecurity []string //不需要做安全检查的URL
	memoryDb   common2.IMemoryDB

	signKey string //signature 服务端密钥
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
		if sf.needSecurity(requestInfo) {
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
			ok, err := sf.antiTampering(requestInfo, c)
			if ok {
				if _, ok2 := errors.AsType[*http.MaxBytesError](err); ok2 {
					basic.GetBaseResponser().AbortWithError(c, http.StatusRequestEntityTooLarge, basic.ConstRequestBodyTooLarge, basic.ConstRequestBodyTooLargeMsg, nil)
					return
				}
				basic.GetBaseResponser().AbortWithError(c, http.StatusForbidden, basic.ConstSignatureUnMatch, basic.ConstSignatureUnMatchMsg, nil)
				return
			}
		}

		// 放行
		c.Next()
	}
}

// ================================= private functions & methods ====================================

func (sf *SecurityFilter) needSecurity(r *basic.RequestInfo) bool {
	// 全局放行（OPTIONS,Swagger/文档类开发端点）不做NONCE/时间戳/签名校验
	if basic.IsGlobalAllowed(r.RequestURL, r.RequestPath, r.RequestMethod) {
		return false
	}

	url := strings.ToLower(r.RequestPath)
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
	diff := currentNow.Timestamp - originalTimestamp

	// 对服务端和客户端的时间差进行判断，是否超过 5 分钟（default）,双向窗口
	if diff > basic.ConstClientServerGap || diff < -basic.ConstClientServerGap {
		return true
	}

	return false
}

func (sf *SecurityFilter) replayAttackNonce(r *basic.RequestInfo) bool {
	// 1. 没有nonce为非法
	tempNonce := strings.TrimSpace(r.Nonce)
	if len(tempNonce) == 0 {
		return true
	}

	if sf.memoryDb != nil {
		// 2. 内存中是否在 5分钟内已经使用过
		nonceKey := basic.NonceKeyPrefix + "_" + tempNonce //Nonce在redis中的key
		_, err := sf.memoryDb.Get(nonceKey)
		if err == nil {
			//过去 2 分钟内使用过
			return true
		}

		// 3. 写入到redis
		_ = sf.memoryDb.Set(nonceKey, tempNonce, time.Duration(basic.MemoryNonceExpiredTime)*time.Second)
		return false
	}

	// memory db不可用，视为无法验证Nonce->拒绝
	return true
}

func (sf *SecurityFilter) antiTampering(r *basic.RequestInfo, c *gin.Context) (bool, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2*1024*1024) //max body size is 2 M
	bodyBytes, err := io.ReadAll(c.Request.Body)
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes)) // 恢复body供后续调用

	if err != nil {
		// 请求体过大错误
		return true, err
	}

	bodyString := string(bodyBytes) //要确保bodyBytes数据不会被修改
	tempStr1 := strconv.FormatInt(r.QueryTimestamp, 10)
	tempStr2 := r.Nonce

	signature := hash.HmacSha256(sf.signKey, bodyString+tempStr1+tempStr2)
	if strings.EqualFold(signature, r.Signature) {
		//此时要放行
		return false, nil
	}

	return true, nil
}
