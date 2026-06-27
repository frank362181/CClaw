package webbase

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/config"
	"my.favorite/platformutility/exception"
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
	certFile, err := configHelper.GetStringValue("server", "certFile")
	if err != nil {
		return err
	}
	if !common.GetOSUtilityInstance().FileExists(certFile) {
		return exception.FilePathErrorEx("cert file not exists:" + certFile)
	}
	keyFile, err := configHelper.GetStringValue("server", "keyFile")
	if err != nil {
		return err
	}
	if !common.GetOSUtilityInstance().FileExists(keyFile) {
		return exception.FilePathErrorEx("key file not exists:" + keyFile)
	}

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
			if router.certFile == "" || router.keyFile == "" {
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

	return true
}

func (router *BaseRouter) Shutdown() error {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := router.Server.Shutdown(ctx); err != nil {
		fmt.Println("Server Shutdown:", err)
	}

	return nil
}
