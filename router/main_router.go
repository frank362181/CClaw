package router

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/config"
	"my.favorite/platformutility/exception"
)

var (
	svrRouter = common.GetSingleton(func() *gin.Engine {
		return buildGinEngine()
	}).Instance()

	server http.Server
)

func SetupFilter(handlerFunc gin.HandlerFunc) {
	svrRouter.Use()
}

// StartServer 启动 Web Service
func StartServer(configFileName string) error {
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

	server := &http.Server{
		Addr:           ":" + serverPort, // port for listening
		Handler:        svrRouter,
		ReadTimeout:    time.Duration(readTimeout) * time.Second,
		WriteTimeout:   time.Duration(writeTimeout) * time.Second,
		IdleTimeout:    time.Duration(idleTimeout) * time.Second,
		MaxHeaderBytes: maxHeaderBytes,
	}

	// 3 start the server
	go func() {
		err := server.ListenAndServeTLS(certFile, keyFile)
		if err != nil {
			os.Exit(-1)
		}
	}()

	return nil
}

func ExitGracefully() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		fmt.Println("Server Shutdown:", err)
	}
}

// ==============================  private functions & methods ==================================
func buildGinEngine() *gin.Engine {
	return gin.New()
}
