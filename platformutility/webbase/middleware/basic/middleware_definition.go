/*
*	middleware 内部使用的各种通用结构体
 */

package basic

import (
	"net/url"
)

type RequestInfo struct {
	RequestId     string
	RemoteIp      string
	RequestURL    string
	RequestMethod string

	RelativeURL string
	QueryString string
	QueryParams url.Values

	ArrivedTimestamp int64 //请求达到的时间

	Nonce string

	//querystring中timestamp的时间戳,请求端放入的,必须是unsigned long(1970-1-1 00:00:00以来的秒数)
	QueryStringTimestamp int64

	Signature string
}

type CommonResponseMsg struct {
	ReqID                string `json:"req_id"`
	ReqMethod            string `json:"req_method"` //用户请求的方法
	RespCode             int    `json:"resp_code"`  //响应码,业务层级
	RespMsg              string `json:"resp_msg"`   //响应的消息
	ReqReceivedTimestamp int64  `json:"req_received_timestamp"`
	ResponseTimestamp    int64  `json:"resp_timestamp"`

	Data interface{} `json:"data"` //具体的相应内容
}
