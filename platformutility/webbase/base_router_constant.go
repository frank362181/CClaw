/*
* 定义常用的常量
 */

package webbase

const BasePath = "/api" //访问的基础路径

const RelativeHealthUrl = "/heartbeat"
const RelativeLoginUrl = "/login"
const RelativeLogoutUrl = "/logout"

const HealthUrl = BasePath + RelativeHealthUrl
const LoginUrl = BasePath + RelativeLoginUrl
const LogoutUrl = BasePath + RelativeLogoutUrl

//const (
//	HttpMethodGet            = "GET"
//	HttpMethodPost           = "POST"
//	HttpMethodPut            = "PUT"
//	HttpMethodPatch          = "PATCH"
//	HttpMethodDelete         = "DELETE"
//	HttpMethodOptions        = "OPTIONS"
//	HttpMethodConnect        = "CONNECT"
//	HttpMethodOptionsConnect = "OPTIONS_CONNECT"
//	HttpMethodUnknown        = ""
//)

const (
	HttpMethodGet            = 1      // get
	HttpMethodPost           = 1 << 1 //post
	HttpMethodPut            = 1 << 2 // put
	HttpMethodPatch          = 1 << 3 // patch
	HttpMethodDelete         = 1 << 4 //delete
	HttpMethodOptions        = 1 << 5 //options
	HttpMethodConnect        = 1 << 6 // connect
	HttpMethodOptionsConnect = 1 << 7 // options_connect
)

var HttpMethods = []int16{
	HttpMethodGet,
	HttpMethodPost,
	HttpMethodPut,
	HttpMethodPatch,
	HttpMethodDelete,
	HttpMethodOptions,
	HttpMethodConnect,
	HttpMethodOptionsConnect,
}

func GetHttpMethod(httpMethod int16) int16 {
	if httpMethod >= HttpMethodGet {
		return httpMethod
	}

	return -1
}
