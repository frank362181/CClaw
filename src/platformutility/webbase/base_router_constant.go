/*
* 定义WEB 服务 URL常用常量
 */

package webbase

// 定义通用的WEB URL常量
const (
	BasePath = "/api" //访问的基础路径

	RelativeHealthUrl = "/heartbeat"
	RelativeLoginUrl  = "/login"
	RelativeLogoutUrl = "/logout"

	HealthUrl = BasePath + RelativeHealthUrl
	LoginUrl  = BasePath + RelativeLoginUrl
	LogoutUrl = BasePath + RelativeLogoutUrl
)

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

var HttpMethodMap = map[int16]string{
	HttpMethodGet:            "GET",
	HttpMethodPost:           "POST",
	HttpMethodPut:            "PUT",
	HttpMethodPatch:          "PATCH",
	HttpMethodDelete:         "DELETE",
	HttpMethodOptions:        "OPTIONS",
	HttpMethodConnect:        "CONNECT",
	HttpMethodOptionsConnect: "OPTIONS_CONNECT",
}

func GetHttpMethodString(httpMethod int16) string {
	if httpMethod >= HttpMethodGet {
		return HttpMethodMap[httpMethod]
	}

	return ""
}
