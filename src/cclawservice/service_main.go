/*
* webservice的入口类
 */

package cclawservice

import (
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	selfController "my.favorite/cclawservice/controller"
	selfInterceptor "my.favorite/cclawservice/interceptor"
	"my.favorite/cclawservice/model/repo"
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

type ServiceMain struct {
	//系统配置
	configFileName string
	configHelper   *config.ConfigurationHelper

	//web服务
	baseRouter *webbase.BaseRouter

	//数据读写相关，RDB,Redis等
	database core.IBaseDatabase
	memoryDB common.IMemoryDB

	// agents related
	defaultAgentConfigHelper *config.ConfigurationHelper
	agentRepo                repo.IAgentRepository
	promptRepo               repo.IPromptRepository
}

// NewMainApp 相当于构造函数
func NewMainApp(confFileName string) *ServiceMain {
	return &ServiceMain{
		configFileName: strings.TrimSpace(confFileName),
		configHelper:   nil,
		baseRouter:     nil,
		database:       nil,
		memoryDB:       nil,

		defaultAgentConfigHelper: nil,
	}
}

func (m *ServiceMain) Init() error {
	if len(m.configFileName) == 0 {
		logging.LogErrorF("[Init] config file name is required,confuse to startup.")
		return exception.ParameterEmptyErrorEx("config file name is required,confuse to startup.")
	}

	// 1. format the configuration filename
	m.configFileName = common2.FormatPath(m.configFileName)

	// 2. load configuration
	var err error
	m.configHelper, err = config.GetConfigurationHelper(m.configFileName)
	if err != nil {
		logging.LogErrorF("[Init] config file %s error: %s,confuse to startup", m.configFileName, err.Error())
		return exception.ConfigErrorEx2("[Init] config file %s error: %s,confuse to startup", m.configFileName, err.Error())
	}

	err = m.configHelper.LoadConfiguration()
	if err != nil {
		logging.LogErrorF("[Init] failed to load %s error: %s,confuse to startup", m.configFileName, err.Error())
		return exception.ConfigErrorEx2("[Init] failed to load %s error: %s,confuse to startup", m.configFileName, err.Error())
	}

	// 3. initialize the app
	m.memoryDB = memory_db.CreateMemoryDb(m.configHelper)
	if m.memoryDB == nil {
		logging.LogErrorF("[Init] memory db init failed")
		return exception.MemoryDbErrEx("memory db init failed")
	}

	m.database = database.GetGormDatabase(m.configHelper)
	err = m.database.Open()
	if err != nil {
		logging.LogErrorF("[Init] failed to open database %s", err.Error())
		return exception.DatabaseErrorEx2("failed to open database,error:%s", err.Error())
	}

	// 4. load the agent configurations
	_ = m.loadAgentConfigurations() //加载失败不影响启动系统

	// 5. initializes the webserver
	m.baseRouter = webbase.GetBaseRouter() //baseRouter必须全局唯一，否则端口会被占用
	if m.baseRouter == nil {
		logging.LogErrorF("[Init] web router init failed")
		return exception.WebServerErrEx("web router init failed")
	}

	m.setGlobalFilter()

	v := m.setupApiFilters()
	if !v {
		return exception.WebServerErrEx("api filters init failed")
	}

	m.setupSwaggerController()

	return nil
}

func (m *ServiceMain) SetupHealthController() error {
	err := m.baseRouter.SetupController(
		webbase.HttpMethodGet,
		cCommon.RelativeHealthUrl,
		controller.GetHealthController().OK,
	)
	if err != nil {
		logging.LogErrorF("[SetupHealthController] failed to setup health controller,err:%s", err.Error())
		return exception.WebServerErrEx2("failed to setup health controller,error:%s", err.Error())
	}

	return nil
}

func (m *ServiceMain) SetupLoginController() error {
	loginService, err := service.GetLoginService(m.memoryDB, m.database) //创建实现登录的业务逻辑对象
	if err != nil {
		logging.LogErrorF("failed to create login service,err:%s", err.Error())
		return exception.WebServerErrEx2("failed to create login service,error:%s", err.Error())
	}
	loginController := selfController.NewLoginController(loginService)

	urlFunMap := make(map[string]gin.HandlerFunc)
	urlFunMap[cCommon.RelativeLoginUrl] = loginController.Login
	urlFunMap[cCommon.RelativeLogoutUrl] = loginController.Logout

	authInter := selfInterceptor.NewAuthenticationInterceptor(loginService)
	tempStr, _ := m.configHelper.GetStringValueDefault("security", "unWantedToken", "")
	urls := strings.Split(tempStr, ",")
	authInter.SetupUnwantedTokenUrls(urls)

	interceptors := interceptor.NewInterceptors()
	err = interceptors.RegistryInterceptor(authInter)
	if err != nil {
		logging.LogErrorF("failed to setup logging controller,err:%s", err.Error())
		urlFunMap = nil
		return exception.WebServerErrEx2("failed to setup logging controller,error:%s", err.Error())
	}

	err = m.baseRouter.SetupControllerEx(webbase.HttpMethodGet|webbase.HttpMethodPost, basic.APIGroupRequire, urlFunMap, interceptors)
	if err != nil {
		logging.LogErrorF("failed to setup logging controller,err:%s", err.Error())
		urlFunMap = nil
		return exception.WebServerErrEx2("failed to setup logging controller,error:%s", err.Error())
	}

	urlFunMap = nil //释放内存
	return nil
}

func (m *ServiceMain) SetupServer() error {
	err := m.baseRouter.SetupServer(m.configHelper)
	if err != nil {
		logging.LogErrorF("[SetupServer] failed to setup server,err:%s", err.Error())
		return exception.WebServerErrEx2("failed to setup server,error:%s", err.Error())
	}

	return nil
}

func (m *ServiceMain) Startup() error {
	sslEnabled, _ := m.configHelper.GetBoolValueDefault("server", "ssl", false)
	m.baseRouter.Startup(sslEnabled)

	return nil
}

func (m *ServiceMain) Shutdown() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
}

func (m *ServiceMain) Destroy() {
	if m.baseRouter != nil {
		_ = m.baseRouter.Shutdown()
	}

	if m.agentRepo != nil {
		m.agentRepo.Destroy()
	}

	if m.promptRepo != nil {
		m.promptRepo.Destroy()
	}

	if m.database != nil {
		m.database.Close()
	}

	if m.memoryDB != nil {
		_ = m.memoryDB.CloseMemoryDb()
	}

	if m.defaultAgentConfigHelper != nil {
		m.defaultAgentConfigHelper.Destroy()
	}

	if m.configHelper != nil {
		m.configHelper.Destroy()
	}
}

// ================================== private function & methods ==========================

func (m *ServiceMain) loadAgentConfigurations() error {
	// 1.首先判断所需的目录是否存在
	workSpaceDir := filepath.Join(common2.GetExecutableDir(), cCommon.AgentServiceWorkspaceDirName)
	_ = common2.CreateDir(workSpaceDir)

	// 2. 加载agent的默认配置信息
	configDir := filepath.Join(common2.GetExecutableDir(), "config")
	defaultAgentConfigFile := filepath.Join(configDir, cCommon.DefaultAgentConfigFile)
	m.defaultAgentConfigHelper, _ = config.GetConfigurationHelper(defaultAgentConfigFile)
	_ = m.defaultAgentConfigHelper.LoadConfiguration()

	// 3. load the agent & LLM providers configuration from the database
	if m.agentRepo == nil {
		m.agentRepo = repo.GetAgentRepository(m.database)
	}
	m.agentRepo.LoadAllAgents()

	// 4. warm up the prompt repository with the default workspace (best-effort)
	if m.promptRepo == nil {
		m.promptRepo = repo.GetPromptRepository()
	}
	if err := m.promptRepo.LoadWorkspace(workSpaceDir); err != nil {
		logging.LogWarnF("warm up default workspace error:%s", err.Error())
	}

	return nil
}

func (m *ServiceMain) setGlobalFilter() {
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
		logging.LogWarnF("[setGlobalFilterAndSwagger] failed to setup global filter,err:%s", err.Error())
	}
}

func (m *ServiceMain) setupApiFilters() bool {
	signKey, _ := m.configHelper.GetStringValueDefault("security", "signKey", "")
	if len(signKey) == 0 {
		logging.LogErrorF("[SetupGlobalFilterAndController] signKey is required,confuse to startup")
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
		logging.LogErrorF("[SetupFilters] failed to setup global filters,err: %s,confuse to start.", err.Error())
		return false
	}

	return true
}

func (m *ServiceMain) setupSwaggerController() {
	swaggerEnabled, _ := m.configHelper.GetBoolValueDefault("server", "enableSwagger", false)
	if swaggerEnabled {
		err := m.baseRouter.SetupController(webbase.HttpMethodGet, "/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
		if err != nil {
			logging.LogWarnF("[setGlobalFilterAndSwagger], failed to setup swagger controller,err:%s", err.Error())
		}
	}
}
