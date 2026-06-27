/*
* 定义middleware中的所有常量
 */

package basic

// ================= 请求判断所需的常量

const ConstClientServerGap int64 = 5 * 60 //服务端与请求端最大的时间间隔

// 定义http中header,request的属性的常量

const ConstRequestTotal string = "RequestTotal"
const ConstRequestId string = "X-REQUEST-ID"
const ConstReqArrivedTimestamp string = "REQ-ARRIVED"
const ConstToken string = "AUTHORIZATION"
const ConstNonce string = "NONCE"
const ConstSignature string = "SIGNATURE"
const ConstQueryStringTimestamp string = "TIMESTAMP"

// ========================= define the const error code & error message ===========================

const (
	ConstSuccessCode         int = 0
	ConstErrorCode           int = 1000 //基础代码
	ConstUrlPatternUnMatched     = ConstErrorCode + 1
	ConstBlackUrl                = ConstErrorCode + 2
	ConstBlackIp                 = ConstErrorCode + 3
	ConstGapGreaterStd           = ConstErrorCode + 4
	ConstNonceInvalid            = ConstErrorCode + 5
	ConstSignatureUnMatched      = ConstErrorCode + 6
	ConstInternalServerError     = ConstErrorCode + 999
)

const (
	ConstSuccess                = "success"
	ConstUrlPatternUnMatchedMsg = "invalid url"
	ConstBlackUrlMsg            = "url hit black list"
	ConstBlackIpMsg             = "IP hit black list"
	ConstGapGreatStdMsg         = "time distance is more greater than standard"
	ConstNonceInvalidMsg        = "Nonce invalid"
	ConstSignatureUnMatchMsg    = "signature unmatched"
	ConstInternalServerErrorMsg = "internal server error"
)
