package webbase

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/config"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
	"my.favorite/platformutility/webbase/middleware/basic"
)

func (router *BaseRouter) SetupFilter(filters ...basic.IFilter) error {
	if filters == nil || len(filters) == 0 {
		return exception.ParameterEmptyErrorEx("filter is empty")
	}

	for _, f := range filters {
		router.filterInterceptorMgr.RegisterFilter(f, router.GinEngine)
	}

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
	writeTimeout, _ := configHelper.GetIntValue("server", "writeTimeout")
	idleTimeout, _ := configHelper.GetIntValue("server", "idleTimeout")
	maxHeaderBytes, _ := configHelper.GetIntValue("server", "maxHeaderBytes")
	certFile, _ := configHelper.GetStringValue("server", "certFile")
	keyFile, _ := configHelper.GetStringValue("server", "keyFile")
	router.certFile = certFile //设置SSL所需的配置文件
	router.keyFile = keyFile

	router.Server = &http.Server{
		Addr:           ":" + serverPort, // port for listening
		Handler:        router.GinEngine,
		ReadTimeout:    time.Duration(readTimeout) * time.Second,
		WriteTimeout:   time.Duration(writeTimeout) * time.Second,
		IdleTimeout:    time.Duration(idleTimeout) * time.Second,
		MaxHeaderBytes: maxHeaderBytes,
	}

	return nil
}

func (router *BaseRouter) Startup(ssl bool) bool {
	if router.Server == nil {
		return false
	}
	// 3 start the server
	go func() {
		if ssl {

			if !common.FileExists(router.certFile) {
				os.Exit(-basic.ConstSSLError)
			}
			if router.certFile == "" || router.keyFile == "" {
				os.Exit(-basic.ConstSSLError)
			}

			if !common.FileExists(router.keyFile) {
				os.Exit(-basic.ConstSSLError)
			}

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

	logging.LogDebug("server started at ", router.Server.Addr)
	return true
}

func (router *BaseRouter) Shutdown() error {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := router.Server.Shutdown(ctx); err != nil {
		logging.LogError("Server force to Shutdown with error: ", err)
	}

	logging.LogInfo("server shutdown complete")
	logging.CloseLogger() //确保日志被顺利关闭
	return nil
}

// =================================== private functions & members ====================================

func (router *BaseRouter) setupController_(httpMethod int16, relativeUrl string, handler gin.HandlerFunc) {
	switch httpMethod {
	case HttpMethodGet:
		router.GinEngine.GET(relativeUrl, handler)
		break
	case HttpMethodPost:
		router.GinEngine.POST(relativeUrl, handler)
		break
	default:
		break
	}
}

func (router *BaseRouter) setupControllerEx_(httpMethod int16, apiGroup string, urlFuncMap map[string]gin.HandlerFunc, inceptors basic.IFilter) error {
	var tempFilter gin.HandlerFunc = nil
	if inceptors != nil {
		tempFilter = inceptors.DoFilter()
	}
	r := router.GinEngine.Group(BasePath, tempFilter)
	if _, ok := router.RGroup[apiGroup]; !ok {
		router.RGroup[apiGroup] = r
	}

	g := router.RGroup[apiGroup]
	{
		for key, value := range urlFuncMap {
			if HttpMethodGet&httpMethod == HttpMethodGet {
				g.GET(key, value)
			}

			if HttpMethodPost&httpMethod == HttpMethodPost {
				g.POST(key, value)
			}
		}
		//iType := *i
		//switch httpMethod {
		//case HttpMethodGet:
		//	switch v := iType.(type) {
		//	case *controller.HealthController:
		//		hc := *v
		//		g.GET(relativeUrl, hc.OK)
		//	default:
		//		break
		//	}
		//case HttpMethodPost:
		//	switch v := iType.(type) {
		//	case *controller.HealthController:
		//		hc := *v
		//		g.POST(relativeUrl, hc.OK)
		//	default:
		//		break
		//	}
		//default:
		//	break
		//}
	}

	return nil
}

//func setupLoginController_(method string, relativeUrl string, i *controller.IController, intercetors *interceptor.Interceptors) error {
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
