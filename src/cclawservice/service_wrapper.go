/*
* webservice的入口类
 */

package cclawservice

import (
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	selfcontroller "my.favorite/cclawservice/controller"
	selfinterceptor "my.favorite/cclawservice/interceptor"
	"my.favorite/cclawservice/service"
	"my.favorite/platformutility/config"
	"my.favorite/platformutility/database"
	"my.favorite/platformutility/database/core"
	"my.favorite/platformutility/logging"
	"my.favorite/platformutility/memory_db"
	"my.favorite/platformutility/memory_db/common"
	"my.favorite/platformutility/webbase"
	"my.favorite/platformutility/webbase/middleware/basic"
	"my.favorite/platformutility/webbase/middleware/controller"
	"my.favorite/platformutility/webbase/middleware/filter"
	"my.favorite/platformutility/webbase/middleware/interceptor"
)

var baseRouter = webbase.GetBaseRouter()
var configHelper *config.ConfigurationHelper
var appDb core.IBaseDatabase //全应用使用的数据库操作实例
var memoryDb common.IMemoryDB

func InitApp(configFileName string) bool {
	configFileName = strings.ToLower(configFileName)
	if len(configFileName) == 0 {
		logging.LogErrorf("config file name can not be empty")
		return false
	}

	var err error
	configHelper, err = config.GetConfigurationHelper(configFileName)
	if err != nil {
		logging.LogErrorf(err.Error())
		return false
	}

	err = configHelper.LoadConfiguration()
	if err != nil {
		logging.LogErrorf(err.Error())
		return false
	}

	memoryDb = memory_db.CreateMemoryDb(configHelper)

	appDb = database.GetGormDatabase(configHelper)
	err = appDb.Open()
	if err != nil {
		return false
	}

	if !setDefaultFilter() {
		return false
	}

	return true
}

func SetupGlobalFilter() {
	// 1. set up the global filter which customized
	corsFilter := filter.GetCorsFilter()
	loggingFilter := filter.GetLoggingFilter()
	reqPreprocessFilter := filter.GetRequestPreprocessFilter()
	addressFilter := filter.GetAddressFilter()
	tempStr, _ := configHelper.GetStringValueDefault("security", "allowedUrl", "")
	urls := strings.Split(tempStr, ",")
	addressFilter.SetupAllowedUrls(urls) // set allowed URLs

	securityFilter := filter.GetSecurityFilter()
	tempStr, _ = configHelper.GetStringValueDefault("security", "deSecurityUrl", "")
	urls = strings.Split(tempStr, ",")
	securityFilter.SetupSecurityFilter(urls) //set security URLS

	SetupFilter(corsFilter, loggingFilter, reqPreprocessFilter, addressFilter, securityFilter)
}

func SetupFilter(filters ...basic.IFilter) {
	err := baseRouter.SetupFilter(filters...)
	if err != nil {
		logging.LogErrorf(err.Error())
		return
	}
}

func SetupHealthController() {
	urlFuncMap := make(map[string]gin.HandlerFunc)
	urlFuncMap[webbase.RelativeHealthUrl] = controller.GetHealthController().OK
	err := baseRouter.SetupControllerEx(webbase.HttpMethodGet, basic.APIGroupCommon, urlFuncMap, nil)
	if err != nil {
		logging.LogErrorf(err.Error())
		urlFuncMap = nil
		return
	}

	urlFuncMap = nil //释放掉内存
}

func SetupLoggingController() {
	userService := service.GetUserService(appDb) //创建实现登录的业务逻辑对象
	loginController := selfcontroller.GetLoginController(userService)

	urlFunMap := make(map[string]gin.HandlerFunc)
	urlFunMap[webbase.RelativeLoginUrl] = loginController.Login
	urlFunMap[webbase.RelativeLogoutUrl] = loginController.Logout

	authInter := selfinterceptor.GetAuthenticationInterceptor(memoryDb, userService)
	tempStr, _ := configHelper.GetStringValueDefault("security", "unWantedToken", "")
	urls := strings.Split(tempStr, ",")
	authInter.SetupUnwantedTokenUrls(urls)

	interceptors := interceptor.GetInterceptors()
	err := interceptors.RegistryInterceptor(authInter)
	if err != nil {
		logging.LogErrorf(err.Error())
		urlFunMap = nil
		return
	}

	err = baseRouter.SetupControllerEx(webbase.HttpMethodGet|webbase.HttpMethodPost, basic.APIGroupRequire, urlFunMap, interceptors)
	if err != nil {
		logging.LogErrorf(err.Error())
		urlFunMap = nil
		return
	}

	urlFunMap = nil //释放内存
}

func SetupServer() bool {
	if baseRouter == nil {
		return false
	}

	err := baseRouter.SetupServer(configHelper)
	if err != nil {
		return false
	}

	return true
}

func Startup() bool {
	if configHelper == nil || baseRouter == nil {
		return false
	}

	sslEnabled, _ := configHelper.GetBoolValueDefault("server", "ssl", false)
	baseRouter.Startup(sslEnabled)

	return true
}

func Shutdown() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	if baseRouter == nil {
		return
	}

	err := baseRouter.Shutdown()
	if err != nil {
		return
	}

	appDb.Close()                //关闭数据库链接
	_ = memoryDb.CloseMemoryDb() //关闭内存链接数据库
}

// =============================== private functions & methods ======================================

func setDefaultFilter() bool {
	if baseRouter == nil {
		baseRouter = webbase.GetBaseRouter()
	}

	if baseRouter == nil {
		logging.LogErrorf("cant not startup the web server")
		return false
	}

	// setup the default filter for global
	err := baseRouter.SetupFilter(filter.GetRecoveryFilter())
	if err != nil {
		logging.LogErrorf(err.Error())
		return false
	}

	// setup the swagger
	err = baseRouter.SetupController(webbase.HttpMethodGet, "/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	if err != nil {
		logging.LogErrorf(err.Error())
		return false
	}

	return true
}
