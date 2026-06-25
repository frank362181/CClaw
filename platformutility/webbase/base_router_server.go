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

func (router *BaseRouter) SetupFilter(filter basic.IFilter) error {
	return nil
}

// StartupServer 启动 Web Service
func (router *BaseRouter) StartupServer(configFileName string) error {
	// 1. read the configuration
	helper, err := config.GetConfigurationHelper(configFileName)
	if err != nil {
		return err
	}

	err = helper.LoadConfiguration()
	if err != nil {
		return err
	}

	// 2. get server config and set them for https
	serverPort, _ := helper.GetStringValue("server", "port")
	readTimeout, _ := helper.GetIntValue("server", "readTimeout")
	writeTimeout, _ := helper.GetIntValue("server", "writeTimeout")
	idleTimeout, _ := helper.GetIntValue("server", "idleTimeout")
	maxHeaderBytes, _ := helper.GetIntValue("server", "maxHeaderBytes")
	certFile, err := helper.GetStringValue("server", "certFile")
	if err != nil {
		return err
	}
	if !common.GetOSUtilityInstance().FileExists(certFile) {
		return exception.FilePathErrorEx("cert file not exists:" + certFile)
	}
	keyFile, err := helper.GetStringValue("server", "keyFile")
	if err != nil {
		return err
	}
	if !common.GetOSUtilityInstance().FileExists(keyFile) {
		return exception.FilePathErrorEx("key file not exists:" + keyFile)
	}

	router.Server = &http.Server{
		Addr:           ":" + serverPort, // port for listening
		Handler:        router.GinEngine,
		ReadTimeout:    time.Duration(readTimeout) * time.Second,
		WriteTimeout:   time.Duration(writeTimeout) * time.Second,
		IdleTimeout:    time.Duration(idleTimeout) * time.Second,
		MaxHeaderBytes: maxHeaderBytes,
	}

	// 3 start the server
	go func() {
		err := router.Server.ListenAndServeTLS(certFile, keyFile)
		if err != nil {
			os.Exit(-1)
		}
	}()

	return nil
}

func (router *BaseRouter) ShutdownServer() error {
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
