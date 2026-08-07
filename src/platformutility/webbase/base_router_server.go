package webbase

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/config"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
	"my.favorite/platformutility/webbase/middleware"
	"my.favorite/platformutility/webbase/middleware/basic"
)

var baseRouter = common.GetSingleton(func() *BaseRouter {
	gin.SetMode(gin.ReleaseMode) //设置release模式

	router := &BaseRouter{
		GinEngine:            gin.New(),
		Server:               nil,
		RGroup:               make(map[string]*gin.RouterGroup),
		filterInterceptorMgr: middleware.GetFilterInterceptorMgr(),
		certFile:             "",
		keyFile:              "",
	}

	_ = router.GinEngine.SetTrustedProxies(nil) //默认不信任任何代理

	return router
}).Instance()

func GetBaseRouter() *BaseRouter {
	return baseRouter
}

func (router *BaseRouter) SetupFilter(filters ...basic.IFilter) error {
	if len(filters) == 0 {
		return exception.ParameterEmptyErrorEx("filter is empty")
	}

	for _, f := range filters {
		router.filterInterceptorMgr.RegisterFilter(f, router.GinEngine)
	}

	return nil
}

func (router *BaseRouter) SetupAPIGroupFilter(apiGroup string, filters ...basic.IFilter) error {
	if len(apiGroup) == 0 || len(filters) == 0 {
		return exception.ParameterEmptyErrorEx("filters is empty")
	}

	handlers := make([]gin.HandlerFunc, 0, len(filters))
	for _, f := range filters {
		if f == nil {
			return exception.ParameterEmptyErrorEx("filter is empty")
		}

		handlers = append(handlers, f.DoFilter())
	}

	router.APIGroup = router.GinEngine.Group(apiGroup, handlers...)
	return nil
}

func (router *BaseRouter) SetupController(httpMethod int16, relativeUrl string, handler gin.HandlerFunc) error {
	method := GetHttpMethod(httpMethod)
	if method == -1 {
		return exception.UnMatchErrorEx("http method unexists")
	}

	if handler == nil {
		return exception.ParameterEmptyErrorEx("handlers is empty")
	}

	router.setupController_(method, relativeUrl, handler)
	return nil
}

func (router *BaseRouter) SetupControllerEx(httpMethod int16, apiGroup string, urlFuncMap map[string]gin.HandlerFunc, interceptors basic.IFilter) error {
	method := GetHttpMethod(httpMethod)
	if method == -1 {
		return exception.UnMatchErrorEx("http method unexists")
	}

	apiGroup = strings.TrimSpace(apiGroup)
	if len(apiGroup) == 0 {
		return exception.ParameterEmptyErrorEx("api group is empty")
	}
	apiGroup = strings.ToLower(apiGroup)

	if len(urlFuncMap) == 0 {
		return exception.ParameterEmptyErrorEx("controllers is empty")
	}

	err := router.setupControllerEx_(method, apiGroup, urlFuncMap, interceptors)
	if err != nil {
		return err
	}
	return nil
}

// SetupServer 设置server
func (router *BaseRouter) SetupServer(configHelper *config.ConfigurationHelper) error {
	if configHelper == nil {
		return exception.ParameterEmptyErrorEx("config for server is empty")
	}

	// 1. get server config and set them for https
	serverPort, _ := configHelper.GetStringValue("server", "port")
	readTimeout, _ := configHelper.GetIntValue("server", "readTimeout")
	readHeaderTimeout, _ := configHelper.GetIntValue("server", "readHeaderTimeout")
	writeTimeout, _ := configHelper.GetIntValue("server", "writeTimeout")
	idleTimeout, _ := configHelper.GetIntValue("server", "idleTimeout")
	maxHeaderBytes, _ := configHelper.GetIntValue("server", "maxHeaderBytes")
	certDir, _ := configHelper.GetStringValueDefault("server", "certDir", "")
	certFile, _ := configHelper.GetStringValue("server", "certFile")
	certFile = filepath.Join(certDir, certFile)
	keyFile, _ := configHelper.GetStringValue("server", "keyFile")
	keyFile = filepath.Join(certDir, keyFile)
	router.certFile = certFile //设置SSL所需的配置文件
	router.keyFile = keyFile

	// 设置可信代理,将来将Nginx/HAProxy等地址填入配置文件即可
	trustedProxies, _ := configHelper.GetStringValueDefault("server", "trustedProxies", "")
	trustedProxies = strings.TrimSpace(trustedProxies)
	if len(trustedProxies) == 0 {
		_ = router.GinEngine.SetTrustedProxies(nil)
	} else {
		proxies := strings.Split(trustedProxies, ",")
		cleaned := make([]string, 0, len(proxies))
		for _, proxy := range proxies {
			cleaned = append(cleaned, strings.TrimSpace(proxy))
		}

		if err := router.GinEngine.SetTrustedProxies(cleaned); err != nil {
			logging.LogErrorf(err.Error())
		}
	}

	///设置可信代理的转发头
	trustedForwardedHeaders, _ := configHelper.GetBoolValueDefault("server", "trustedForwardedHeaders", false)
	basic.GetBaseRequester().SetTrustedForwardedHeaders(trustedForwardedHeaders)

	router.Server = &http.Server{
		Addr:              ":" + serverPort, // port for listening
		Handler:           router.GinEngine,
		ReadTimeout:       time.Duration(readTimeout) * time.Second,
		ReadHeaderTimeout: time.Duration(readHeaderTimeout) * time.Second,
		WriteTimeout:      time.Duration(writeTimeout) * time.Second,
		IdleTimeout:       time.Duration(idleTimeout) * time.Second,
		MaxHeaderBytes:    maxHeaderBytes,
	}

	return nil
}

func (router *BaseRouter) Startup(ssl bool) bool {
	if router.Server == nil {
		return false
	}
	// 3 start the server
	if ssl {
		if !common.FileExists(router.certFile) || !common.FileExists(router.keyFile) {
			logging.LogInfof("[Startup] SSL certificate file %s or %s does not exist", router.certFile, router.keyFile)
			return false
		}
	}
	//启动服务
	go func() {
		if ssl {
			err := router.Server.ListenAndServeTLS(router.certFile, router.keyFile)
			if err != nil {
				os.Exit(-basic.ConstInternalServerError)
			}
		} else {
			err := router.Server.ListenAndServe()
			if err != nil {
				os.Exit(-basic.ConstInternalServerError)
			}
		}
	}()

	logging.LogDebugf("server started at:%s", router.Server.Addr)
	return true
}

func (router *BaseRouter) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := router.Server.Shutdown(ctx); err != nil {
		logging.LogErrorf("Server force to Shutdown with error: %s", err.Error())
	}

	logging.LogInfof("server shutdown complete")
	return nil
}

// =================================== private functions & members ====================================

func (router *BaseRouter) setupController_(httpMethod int16, relativeUrl string, handler gin.HandlerFunc) {
	switch httpMethod {
	case HttpMethodGet:
		router.GinEngine.GET(relativeUrl, handler)
		//break
	case HttpMethodPost:
		router.GinEngine.POST(relativeUrl, handler)
		//break
	default:
		break
	}
}

func (router *BaseRouter) setupControllerEx_(httpMethod int16, apiGroup string, urlFuncMap map[string]gin.HandlerFunc, inceptors basic.IFilter) error {
	g, ok := router.RGroup[apiGroup]
	if !ok {
		if inceptors == nil {
			g = router.APIGroup.Group("")
		} else {
			g = router.APIGroup.Group("", inceptors.DoFilter())
		}

		router.RGroup[apiGroup] = g
	}

	for relativeUrl, handler := range urlFuncMap {
		if HttpMethodGet&httpMethod == HttpMethodGet {
			g.GET(relativeUrl, handler)
		}

		if HttpMethodPost&httpMethod == HttpMethodPost {
			g.POST(relativeUrl, handler)
		}
	}

	return nil
}

//func setupLoginController_(method string, relativeUrl string, i *controller.IController, interceptors *interceptor.Interceptors) error {
//	typeName := (*i).GetType()
//	if _, ok := baseRouter.RGroup[typeName]; !ok {
//		r := baseRouter.GinEngine.Group(typeName)
//		baseRouter.RGroup[typeName] = r
//	}
//
//	g := baseRouter.RGroup[typeName]
//	{
//		iType := *i
//		switch method {
//		case webbase.HttpMethodGet:
//			switch v := iType.(type) {
//			case *contnroller.LoginController:
//				hc := *v
//				g.GET(relativeUrl, hc.Login)
//			default:
//				break
//			}
//		case webbase.HttpMethodPost:
//			switch v := iType.(type) {
//			case *contnroller.LoginController:
//				hc := *v
//				g.POST(relativeUrl, hc.Logout)
//			default:
//				break
//			}
//		default:
//			break
//		}
//	}
//
//	return nil
//}
