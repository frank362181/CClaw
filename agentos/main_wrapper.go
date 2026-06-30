/*
* 定义main的入口函数和逻辑
 */

package agentos

import (
	"strings"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"my.favorite/agentos/agentoscontroller"
	"my.favorite/agentos/agentosinterceptor"
	"my.favorite/agentos/agentosservice"
	"my.favorite/platformutility/config"
	"my.favorite/platformutility/logging"
	"my.favorite/platformutility/webbase"
	"my.favorite/platformutility/webbase/middleware/basic"
	"my.favorite/platformutility/webbase/middleware/controller"
	"my.favorite/platformutility/webbase/middleware/filter"
	"my.favorite/platformutility/webbase/middleware/interceptor"
)

var baseRouter = webbase.GetBaseRouter()
var configHelper *config.ConfigurationHelper

func InitApp(configFileName string) bool {
	configFileName = strings.ToLower(configFileName)
	if len(configFileName) == 0 {
		logging.LogError("config file name can not be empty")
		return false
	}

	tempHelper, err := config.GetConfigurationHelper(configFileName)
	if err != nil {
		logging.LogError(err.Error())
		return false
	}

	err = tempHelper.LoadConfiguration()
	if err != nil {
		logging.LogError(err.Error())
		return false
	}

	if !setDefaultFilter() {
		return false
	}

	configHelper = tempHelper
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
		logging.LogError(err.Error())
		return
	}
}

func SetupHealthController() {
	urlFuncMap := make(map[string]gin.HandlerFunc)
	urlFuncMap[webbase.RelativeHealthUrl] = controller.GetHealthController().OK
	err := baseRouter.SetupControllerEx(webbase.HttpMethodGet, basic.APIGroupCommon, urlFuncMap, nil)
	if err != nil {
		logging.LogError(err.Error())
		urlFuncMap = nil
		return
	}

	urlFuncMap = nil //释放掉内存
}

func SetupLoggingController() {
	loginService := agentosservice.GetLoginService()

	urlFunMap := make(map[string]gin.HandlerFunc)

	urlFunMap[webbase.RelativeLoginUrl] = agentoscontroller.GetLoginController(loginService).Login
	urlFunMap[webbase.RelativeLogoutUrl] = agentoscontroller.GetLoginController(loginService).Logout

	authInter := agentosinterceptor.GetAuthenticationInterceptor()
	tempStr, _ := configHelper.GetStringValueDefault("security", "unWantedToken", "")
	urls := strings.Split(tempStr, ",")
	authInter.SetupUnwantedTokenUrls(urls)

	interceptors := interceptor.GetInterceptors()
	err := interceptors.RegistryInterceptor(authInter)
	if err != nil {
		logging.LogError(err.Error())
		urlFunMap = nil
		return
	}

	err = baseRouter.SetupControllerEx(webbase.HttpMethodGet|webbase.HttpMethodPost, basic.APIGroupRequire, urlFunMap, interceptors)
	if err != nil {
		logging.LogError(err.Error())
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
	if baseRouter == nil {
		return
	}

	err := baseRouter.Shutdown()
	if err != nil {
		return
	}
}

// =============================== private functions & methods ======================================

func setDefaultFilter() bool {
	if baseRouter == nil {
		baseRouter = webbase.GetBaseRouter()
	}

	if baseRouter == nil {
		logging.LogError("cant not startup the web server")
		return false
	}

	// setup the default filter for global
	err := baseRouter.SetupFilter(filter.GetRecoveryFilter())
	if err != nil {
		logging.LogError(err.Error())
		return false
	}

	// setup the swagger
	err = baseRouter.SetupController(webbase.HttpMethodGet, "/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	if err != nil {
		logging.LogError(err.Error())
		return false
	}

	return true
}
