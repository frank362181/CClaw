/*
* 定义middleware中的所有常量
 */

package basic

// ================= web service级通用常量===========================

const (
	TrafficFilterTimeGap = 2 * 60 //流量控制的时间间隔
)

const (
	IPTrafficKeyPrefix = "ip_traffic_"
	IPAPITrafficPrefix = "ip_api_traffic_"
)

const (
	IPTrafficFilterLimits    = 1000 // 同一个 IP 指定时间范围内 1000次
	IPAPITrafficFilterLimits = 50   // 同一个用户同一个API指定时间内 50次
)

const (
	APIGroupCommon  = "common" //不需要interceptor过滤的control
	APIGroupRequire = "require"
)

const (
	FilterOrderRecovery = iota + 1 //最外层的Filter
	FilterOrderCors
	FilterOrderLogging // 日志filter
	FilterOrderReqPre  //预处理
	FilterOrderAddress //地址处理
	FilterOrderTraffic
	FilterOrderSecurity //安全处理
)

const ConstClientServerGap int64 = 5 * 60 //服务端与请求端最大的时间间隔

// 定义http中header,request的属性的常量

const ConstRequestTotal string = "RequestTotal"

const ConstHeaderToken string = "AUTHORIZATION"

const ConstQueryNonce string = "NONCE"
const ConstSignature string = "SIGNATURE"
const ConstQueryTimestamp string = "TIMESTAMP"

// ========================= define the const error code & error message ===========================

const (
	ConstSuccessCode int = 0

	ConstErrorCode int = iota + 1000 //基础代码
	ConstSchemaUnsupported
	ConstUrlInvalid
	ConstBlackUrl
	ConstBlackIp
	ConstNonceInvalid
	ConstSignatureUnexists
	ConstSignatureUnMatch
	ConstTimeGapOverflow
	ConstInternalServerError
	ConstTrafficOverflow

	ConstSSLError

	ConstRequestBodyTooLarge

	ConstErrorMax //以上定义的是内部的常量,外部常量从这个开始
)

const (
	ConstSuccess = "success"

	ConstSchemaUnsupportedMsg   = "schema unsupported"
	ConstUrlInvalidMsg          = "invalid url"
	ConstBlackUrlMsg            = "url hit black list"
	ConstBlackIpMsg             = "IP hit black list"
	ConstNonceInvalidMsg        = "Nonce invalid"
	ConstSignatureUnexistsMsg   = "header signature unexists"
	ConstSignatureUnMatchMsg    = "signature unmatched"
	ConstTimeGapOverflowMsg     = "time gap overflow"
	ConstInternalServerErrorMsg = "internal server error"
	ConstTrafficOverflowMsg     = "traffic overflow"
	ConstRequestBodyTooLargeMsg = "request body too large"
)
