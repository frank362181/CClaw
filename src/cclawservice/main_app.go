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
	selfController "my.favorite/cclawservice/controller"
	selfInterceptor "my.favorite/cclawservice/interceptor"
	"my.favorite/cclawservice/service"
	common2 "my.favorite/platformutility/common"
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

type MainApp struct {
	configFileName string
	configHelper   *config.ConfigurationHelper
	baseRouter     *webbase.BaseRouter

	database core.IBaseDatabase
	memoryDB common.IMemoryDB
}

// NewMainApp 相当于构造函数
func NewMainApp(confFileName string) *MainApp {
	return &MainApp{
		configFileName: strings.TrimSpace(confFileName),
		configHelper:   nil,
		baseRouter:     nil,
		database:       nil,
		memoryDB:       nil,
	}
}

func (m *MainApp) Init() bool {
	if len(m.configFileName) == 0 {
		logging.LogErrorf("[Init] config file name is required,confuse to startup.")
		return false
	}

	// 1. format the configuration filename
	m.configFileName = common2.FormatPath(m.configFileName)

	// 2. load configuration
	var err error
	m.configHelper, err = config.GetConfigurationHelper(m.configFileName)
	if err != nil {
		logging.LogErrorf("[Init] config file %s error: %s,confuse to startup", m.configFileName, err.Error())
		return false
	}

	err = m.configHelper.LoadConfiguration()
	if err != nil {
		logging.LogErrorf("[Init] failed to load %s error: %s,confuse to startup", m.configFileName, err.Error())
		return false
	}

	// 3. initialize the app
	m.memoryDB = memory_db.CreateMemoryDb(m.configHelper)
	if m.memoryDB == nil {
		logging.LogErrorf("[Init] memory db init failed")
		return false
	}

	m.database = database.GetGormDatabase(m.configHelper)
	err = m.database.Open()
	if err != nil {
		logging.LogErrorf("[Init] failed to open database %s", err.Error())
		return false
	}

	m.baseRouter = webbase.GetBaseRouter() //baseRouter必须全局唯一，否则端口会被占用
	if m.baseRouter == nil {
		logging.LogErrorf("[Init] web router init failed")
		return false
	}

	m.setGlobalFilter()

	v := m.setupApiFilters()
	if !v {
		return false
	}

	m.setupSwaggerController()

	return true
}

func (m *MainApp) SetupHealthController() bool {
	err := m.baseRouter.SetupController(
		webbase.HttpMethodGet,
		webbase.RelativeHealthUrl,
		controller.GetHealthController().OK,
	)
	if err != nil {
		logging.LogErrorf("[SetupHealthController] failed to setup health controller,err:%s", err.Error())
		return false
	}

	return true
}

func (m *MainApp) SetupLoggingController() bool {
	userService := service.GetUserService(m.memoryDB, m.database) //创建实现登录的业务逻辑对象
	loginController := selfController.GetLoginController(userService)

	urlFunMap := make(map[string]gin.HandlerFunc)
	urlFunMap[webbase.RelativeLoginUrl] = loginController.Login
	urlFunMap[webbase.RelativeLogoutUrl] = loginController.Logout

	authInter := selfInterceptor.GetAuthenticationInterceptor(userService)
	tempStr, _ := m.configHelper.GetStringValueDefault("security", "unWantedToken", "")
	urls := strings.Split(tempStr, ",")
	authInter.SetupUnwantedTokenUrls(urls)

	interceptors := interceptor.GetInterceptors()
	err := interceptors.RegistryInterceptor(authInter)
	if err != nil {
		logging.LogErrorf("failed to setup logging controller,err:%s", err.Error())
		urlFunMap = nil
		return false
	}

	err = m.baseRouter.SetupControllerEx(webbase.HttpMethodGet|webbase.HttpMethodPost, basic.APIGroupRequire, urlFunMap, interceptors)
	if err != nil {
		logging.LogErrorf("failed to setup logging controller,err:%s", err.Error())
		urlFunMap = nil
		return false
	}

	urlFunMap = nil //释放内存
	return true
}

func (m *MainApp) SetupServer() bool {
	err := m.baseRouter.SetupServer(m.configHelper)
	if err != nil {
		logging.LogErrorf("[SetupServer] failed to setup server,err:%s", err.Error())
		return false
	}

	return true
}

func (m *MainApp) Startup() bool {
	sslEnabled, _ := m.configHelper.GetBoolValueDefault("server", "ssl", false)
	m.baseRouter.Startup(sslEnabled)

	return true
}

func (m *MainApp) Shutdown() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
}

func (m *MainApp) Destroy() {
	if m.baseRouter != nil {
		_ = m.baseRouter.Shutdown()
	}

	if m.database != nil {
		m.database.Close()
	}

	if m.memoryDB != nil {
		m.memoryDB.CloseMemoryDb()
	}
}

// ================================== private function & methods ==========================

func (m *MainApp) setGlobalFilter() {
	originsStr, _ := m.configHelper.GetStringValueDefault("security", "allowedOrigins", "")
	var origins []string
	if len(originsStr) > 0 {
		splits := strings.Split(originsStr, ",")
		for _, s := range splits {
			origins = append(origins, strings.TrimSpace(s))
		}
	}
	corsFilter := filter.GetCorsFilter()
	corsFilter.SetupAllowedOrigins2(origins)

	recoveryFilter := filter.GetRecoveryFilter()

	err := m.baseRouter.SetupFilter(recoveryFilter, corsFilter) //注册全局filter
	if err != nil {
		logging.LogWarnf("[setGlobalFilterAndSwagger] failed to setup global filter,err:%s", err.Error())
	}
}

func (m *MainApp) setupApiFilters() bool {

	signKey, _ := m.configHelper.GetStringValueDefault("security", "signKey", "")
	if len(signKey) == 0 {
		logging.LogErrorf("[SetupGlobalFilterAndController] signKey is required,confuse to startup")
		return false
	}

	// 1 setup business filter
	loggingFilter := filter.GetLoggingFilter()
	reqPreprocessFilter := filter.GetRequestPreprocessFilter()
	addressFilter := filter.GetAddressFilter()
	tempStr, _ := m.configHelper.GetStringValueDefault("security", "allowedUrl", "")
	urls := strings.Split(tempStr, ",")
	addressFilter.SetupAllowedUrls(urls)

	trafficFilter := filter.GetTrafficFilter(m.memoryDB)

	securityFilter := filter.GetSecurityFilter(m.memoryDB, signKey)
	tempStr, _ = m.configHelper.GetStringValueDefault("security", "deSecurityUrl", "")
	urls = strings.Split(tempStr, ",")
	securityFilter.SetupDeSecurityFilter(urls)

	err := m.baseRouter.SetupAPIGroupFilter(webbase.BasePath, loggingFilter, reqPreprocessFilter, addressFilter, trafficFilter, securityFilter)
	if err != nil {
		logging.LogErrorf("[SetupFilters] failed to setup global filters,err: %s,confuse to start.", err.Error())
		return false
	}

	return true
}

func (m *MainApp) setupSwaggerController() {
	swaggerEnabled, _ := m.configHelper.GetBoolValueDefault("server", "enableSwagger", false)
	if swaggerEnabled {
		err := m.baseRouter.SetupController(webbase.HttpMethodGet, "/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
		if err != nil {
			logging.LogWarnf("[setGlobalFilterAndSwagger], failed to setup swagger controller,err:%s", err.Error())
		}
	}
}
