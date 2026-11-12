package webbase

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	selfconfig "my.favorite/cclawservice/config"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/common/hash"
	"my.favorite/platformutility/concurrency"
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
		_ = router.filterInterceptorMgr.RegisterFilter(f, router.GinEngine)
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
func (router *BaseRouter) SetupServer(serviceConfig selfconfig.ServiceConfig) error {
	router.certFile = serviceConfig.Server.CertFile //设置SSL所需的配置文件
	router.keyFile = serviceConfig.Server.KeyFile

	// 设置可信代理,将来将Nginx/HAProxy等地址填入配置文件即可
	if len(serviceConfig.Server.TrustedProxies) == 0 {
		_ = router.GinEngine.SetTrustedProxies(nil)
	} else {
		if err := router.GinEngine.SetTrustedProxies(serviceConfig.Server.TrustedProxies); err != nil {
			logging.LogErrorF(err.Error())
		}
	}

	///设置可信代理的转发头
	basic.GetBaseRequester().SetTrustedForwardedHeaders(serviceConfig.Server.TrustedForwardedHeaderEnabled)

	router.Server = &http.Server{
		Addr:              ":" + serviceConfig.Server.Port, // port for listening
		Handler:           router.GinEngine,
		ReadTimeout:       time.Duration(serviceConfig.Server.ReadTimeout) * time.Second,
		ReadHeaderTimeout: time.Duration(serviceConfig.Server.ReadHeaderTimeout) * time.Second,
		WriteTimeout:      time.Duration(serviceConfig.Server.WriteTimeout) * time.Second,
		IdleTimeout:       time.Duration(serviceConfig.Server.IdleTimeout) * time.Second,
		MaxHeaderBytes:    serviceConfig.Server.MaxHeaderBytes,
	}

	return nil
}

// Startup 启动 HTTP/HTTPS 服务
// 返回 true 表示监听端口绑定成功并已进入服务状态；false 表示启动失败（证书缺失/无效、端口被占用等）
func (router *BaseRouter) Startup(ssl bool) bool {
	if router.Server == nil {
		return false
	}

	// 同步校验证书：缺失/无效立即失败，避免进程启动后才暴露证书问题
	if ssl {
		if !common.FileExists(router.certFile) || !common.FileExists(router.keyFile) {
			logging.LogErrorF("[Startup] SSL certificate file %s or %s does not exist", router.certFile, router.keyFile)
			return false
		}
		if _, err := hash.LoadX509KeyPair(router.certFile, router.keyFile); err != nil {
			logging.LogErrorF("[Startup] invalid SSL certificate/key: %s", err.Error())
			return false
		}
	}

	// 同步绑定监听端口：绑定失败（如端口被占用）立即返回失败，避免进程「假活」（无监听却继续运行）
	listener, err := net.Listen("tcp", router.Server.Addr)
	if err != nil {
		logging.LogErrorF("[Startup] can not listen on %s: %s", router.Server.Addr, err.Error())
		return false
	}

	router.httpServerScope = concurrency.GetTaskManager().NewScope("http-server", 1)
	if err := router.httpServerScope.Go(context.Background(), concurrency.RoutineSpec{
		Name: "http-server",
		Fn: func(_ context.Context) error {
			var serveErr error
			if ssl {
				serveErr = router.Server.ServeTLS(listener, router.certFile, router.keyFile)
			} else {
				serveErr = router.Server.Serve(listener)
			}
			if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				logging.LogErrorF("[Startup] server at %s stopped with error: %s", router.Server.Addr, serveErr.Error())
			}
			return nil
		},
	}); err != nil {
		_ = listener.Close() // 任务没有起来,监听必须自己关闭
		logging.LogErrorF("[Startup] server at %s stop with: %s", router.Server.Addr, err.Error())
		return false
	}

	logging.LogDebugF("server started at:%s,time = %s", router.Server.Addr, common.GetDatetimeUtilityInstance().GetCurrentDatetime().DatetimeStr)
	return true
}

func (router *BaseRouter) Shutdown() error {
	// 防御：未完成 SetupServer 或启动失败时 Server 可能为 nil，避免清理时空指针崩溃
	if router == nil || router.Server == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := router.Server.Shutdown(ctx); err != nil {
		logging.LogWarnF("Server shutdown with in-flight long connections (expected for SSE): %s", err.Error())
	}

	// shutdown 只保证"不再接受新链接 + 在飞链接处理完"
	// 但server 协程本身要再调度一次才会返回,这里等它,避免后续关闭DB时还在运行
	if router.httpServerScope != nil {
		// 这里大概率会空转
		drainCtx, cancelDrain := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelDrain()

		if err := router.httpServerScope.Wait(drainCtx); err != nil {
			logging.LogWarnF("http server task not drained with 5s: %s", err.Error())
		}
	}

	logging.LogInfoF("server shutdown complete")
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
