/*
* 定义middleware中的所有常量
 */

package basic

// ================= 请求判断所需的常量

const (
	APIGroupCommon  = "common" //不需要interceptor过滤的control
	APIGroupRequire = "require"
)

const (
	FilterOrderRecovery = iota + 1 //最外层的Filter
	FilterOrderCors
	FilterOrderLogging  // 日志filter
	FilterOrderReqPre   //预处理
	FilterOrderAddress  //地址处理
	FilterOrderSecurity //安全处理
)

const ConstClientServerGap int64 = 5 * 60 //服务端与请求端最大的时间间隔

// 定义http中header,request的属性的常量

const ConstRequestTotal string = "RequestTotal"

const ConstHeaderRequestID string = "X-REQUEST-ID"
const ConstHeaderToken string = "AUTHORIZATION"
const ConstHeaderTimestamp = "TIMESTAMP"

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
	ConstHeaderSignatureUnexists
	ConstHeaderSignatureUnMatch
	ConstHeaderTimestampUnexists
	ConstHeaderTimestampUnMatch
	ConstInternalServerError

	ConstSSLError

	ConstErrorMax //以上定义的是内部的常量,外部常量从这个开始
)

const (
	ConstSuccess = "success"

	ConstSchemaUnsupportedMsg       = "schema unsupported"
	ConstUrlInvalidMsg              = "invalid url"
	ConstBlackUrlMsg                = "url hit black list"
	ConstBlackIpMsg                 = "IP hit black list"
	ConstNonceInvalidMsg            = "Nonce invalid"
	ConstHeaderSignatureUnexistsMsg = "header signature unexists"
	ConstHeaderSignatureUnMatchMsg  = "signature unmatched"
	ConstHeaderTimestampUnexistsMsg = "header timestamp unexists"
	ConstHeaderTimestampUnMatchMsg  = "timestamp unmatched"
	ConstInternalServerErrorMsg     = "internal server error"
)
