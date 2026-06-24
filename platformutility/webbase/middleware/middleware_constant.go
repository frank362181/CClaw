/*
* 定义middleware中的所有常量
 */

package middleware

// ================= 请求判断所需的常量

const ConstClientServerGap int64 = 5 * 60 //服务端与请求端最大的时间间隔

// 定义http中header,request的属性的常量

const ConstRequestTotal string = "RequestTotal"

const ConstRequestId string = "X-REQUEST-ID"
const ConstReqArrivedTimestamp string = "REQ-ARRIVED"
const ConstToken string = "AUTHORIZATION-TOKEN"
const ConstNonce string = "NONCE"
const ConstSignature string = "SIGNATURE"
const ConstQueryStringTimestamp string = "TIMESTAMP"

// ========================= define the const error code ===========================

const ConstErrorCode int = 1000 //基础代码
const ConstUrlPatternUnMatched int = ConstErrorCode + 1
const ConstBlackUrl int = ConstErrorCode + 2
const ConstBlackIp int = ConstErrorCode + 3
