/*
* webservice的入口类
 */

package cclawservice

import (
	"errors"
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
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
	"my.favorite/platformutility/memory_db"
	"my.favorite/platformutility/memory_db/common"
	"my.favorite/platformutility/webbase"
	"my.favorite/platformutility/webbase/middleware/basic"
	"my.favorite/platformutility/webbase/middleware/controller"
	"my.favorite/platformutility/webbase/middleware/filter"
	"my.favorite/platformutility/webbase/middleware/interceptor"

	cCommon "my.favorite/cclawservice/common"
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

func (m *MainApp) Init() error {
	if len(m.configFileName) == 0 {
		logging.LogErrorf("[Init] config file name is required,confuse to startup.")
		return exception.ParameterEmptyErrorEx("config file name is required,confuse to startup.")
	}

	// 1. format the configuration filename
	m.configFileName = common2.FormatPath(m.configFileName)

	// 2. load configuration
	var err error
	m.configHelper, err = config.GetConfigurationHelper(m.configFileName)
	if err != nil {
		logging.LogErrorf("[Init] config file %s error: %s,confuse to startup", m.configFileName, err.Error())
		return errors.New("config file " + m.configFileName + " error: " + err.Error())
	}

	err = m.configHelper.LoadConfiguration()
	if err != nil {
		logging.LogErrorf("[Init] failed to load %s error: %s,confuse to startup", m.configFileName, err.Error())
		return errors.New("failed to load " + m.configFileName + " error: " + err.Error())
	}

	// 3. initialize the app
	m.memoryDB = memory_db.CreateMemoryDb(m.configHelper)
	if m.memoryDB == nil {
		logging.LogErrorf("[Init] memory db init failed")
		return errors.New("memory db init failed")
	}

	m.database = database.GetGormDatabase(m.configHelper)
	err = m.database.Open()
	if err != nil {
		logging.LogErrorf("[Init] failed to open database %s", err.Error())
		return errors.New("failed to open database " + err.Error())
	}

	m.baseRouter = webbase.GetBaseRouter() //baseRouter必须全局唯一，否则端口会被占用
	if m.baseRouter == nil {
		logging.LogErrorf("[Init] web router init failed")
		return errors.New("web router init failed")
	}

	m.setGlobalFilter()

	v := m.setupApiFilters()
	if !v {
		return errors.New("api filters init failed")
	}

	m.setupSwaggerController()

	return nil
}

func (m *MainApp) SetupHealthController() error {
	err := m.baseRouter.SetupController(
		webbase.HttpMethodGet,
		cCommon.RelativeHealthUrl,
		controller.GetHealthController().OK,
	)
	if err != nil {
		logging.LogErrorf("[SetupHealthController] failed to setup health controller,err:%s", err.Error())
		return errors.New("failed to setup health controller: " + err.Error())
	}

	return nil
}

func (m *MainApp) SetupLoginController() error {
	userService, err := service.GetLoginService(m.memoryDB, m.database) //创建实现登录的业务逻辑对象
	if err != nil {
		logging.LogErrorf("failed to create login service,err:%s", err.Error())
		return errors.New("failed to create login service: " + err.Error())
	}
	loginController := selfController.NewLoginController(userService)

	urlFunMap := make(map[string]gin.HandlerFunc)
	urlFunMap[cCommon.RelativeLoginUrl] = loginController.Login
	urlFunMap[cCommon.RelativeLogoutUrl] = loginController.Logout

	authInter := selfInterceptor.NewAuthenticationInterceptor(userService)
	tempStr, _ := m.configHelper.GetStringValueDefault("security", "unWantedToken", "")
	urls := strings.Split(tempStr, ",")
	authInter.SetupUnwantedTokenUrls(urls)

	interceptors := interceptor.NewInterceptors()
	err = interceptors.RegistryInterceptor(authInter)
	if err != nil {
		logging.LogErrorf("failed to setup logging controller,err:%s", err.Error())
		urlFunMap = nil
		return errors.New("failed to setup logging controller: " + err.Error())
	}

	err = m.baseRouter.SetupControllerEx(webbase.HttpMethodGet|webbase.HttpMethodPost, basic.APIGroupRequire, urlFunMap, interceptors)
	if err != nil {
		logging.LogErrorf("failed to setup logging controller,err:%s", err.Error())
		urlFunMap = nil
		return errors.New("failed to setup logging controller: " + err.Error())
	}

	urlFunMap = nil //释放内存
	return nil
}

func (m *MainApp) SetupServer() error {
	err := m.baseRouter.SetupServer(m.configHelper)
	if err != nil {
		logging.LogErrorf("[SetupServer] failed to setup server,err:%s", err.Error())
		return errors.New("failed to setup server: " + err.Error())
	}

	return nil
}

func (m *MainApp) Startup() error {
	sslEnabled, _ := m.configHelper.GetBoolValueDefault("server", "ssl", false)
	m.baseRouter.Startup(sslEnabled)

	return nil
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
		_ = m.memoryDB.CloseMemoryDb()
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
	loggingFilter := filter.NewLoggingFilter(basic.FilterOrderLogging)                //filter.GetLoggingFilter()
	reqPreprocessFilter := filter.NewRequestPreprocessFilter(basic.FilterOrderReqPre) //filter.GetRequestPreprocessFilter()
	addressFilter := filter.NewAddressFilter(basic.FilterOrderAddress)                //filter.GetAddressFilter()
	tempStr, _ := m.configHelper.GetStringValueDefault("security", "allowedUrl", "")
	urls := strings.Split(tempStr, ",")
	addressFilter.SetupAllowedUrls(urls)

	trafficFilter := filter.NewTrafficFilter(basic.FilterOrderTraffic, m.memoryDB) //filter.GetTrafficFilter(m.memoryDB)

	securityFilter := filter.NewSecurityFilter(basic.FilterOrderSecurity, m.memoryDB, signKey) //filter.GetSecurityFilter(m.memoryDB, signKey)
	tempStr, _ = m.configHelper.GetStringValueDefault("security", "deSecurityUrl", "")
	urls = strings.Split(tempStr, ",")
	securityFilter.SetupDeSecurityFilter(urls)

	err := m.baseRouter.SetupAPIGroupFilter(cCommon.BasePath, loggingFilter, reqPreprocessFilter, addressFilter, trafficFilter, securityFilter)
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
