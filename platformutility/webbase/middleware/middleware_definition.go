/*
*	middleware 内部使用的各种通用结构体
 */

package middleware

import (
	"net/url"
	"time"
)

type RequestInfo struct {
	RequestId     string
	RemoteIp      string
	RequestURL    string
	RequestMethod string

	RelativeURL string
	QueryString string
	QueryParams url.Values

	ArrivedTimestamp time.Time //请求达到的时间

	Nonce string

	//querystring中timestamp的时间戳,请求端放入的,必须是unsigned long(1970-1-1 00:00:00以来的秒数)
	QueryStringTimestamp int64

	Signature string
}
