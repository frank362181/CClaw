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
	"my.favorite/cclawservice/chat"
	selfController "my.favorite/cclawservice/controller"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/harness/observatory"
	selfInterceptor "my.favorite/cclawservice/interceptor"
	"my.favorite/cclawservice/model/repo"
	"my.favorite/cclawservice/service"
	core2 "my.favorite/cclawservice/service/core"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/concurrency"
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
	agentRepo                *repo.AgentRepository
	agentFileRepo            *ctxmgr.AgentFileRepository
	agentDocCache            *ctxmgr.AgentDocumentCache

	// 并发闸:流式(SSE)在飞连接数、全局聊天在飞请求数(各自上限在 Init 按 ini 配置)
	concurrent concurrency.IConcurrencyController
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
	m.configFileName = filepath.ToSlash(m.configFileName)

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

	// 并发闸上限(ini [server],<=0 用默认):全局并发限制 + 流式并发限制
	//maxStreams, _ := m.configHelper.GetIntValueDefault("server", "maxStreamConcurrency", cCommon.DefaultMaxStreamConcurrency)
	maxConcurrency, _ := m.configHelper.GetIntValueDefault("server", "maxChatConcurrency", cCommon.DefaultMaxChatConcurrency)
	m.concurrent = concurrency.GetConcurrentController(maxConcurrency) //serviceCore.GetStreamConcurrencyGate(maxStreams)

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

	observatory.RegisterTokenObserver()  //注册token用量观测函数
	observatory.RegisterTimingObserver() //注册时间维度的可观测函数

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
	loginController := selfController.GetLoginController(loginService)

	urlFunMap := make(map[string]gin.HandlerFunc)
	urlFunMap[cCommon.RelativeLoginUrl] = loginController.Login
	urlFunMap[cCommon.RelativeLogoutUrl] = loginController.Logout

	authInter := selfInterceptor.GetAuthenticationInterceptor(loginService)
	tempStr, _ := m.configHelper.GetStringValueDefault("security", "unWantedToken", "")
	urls := strings.Split(tempStr, ",")
	authInter.SetupUnwantedTokenUrls(urls)

	interceptors := interceptor.GetInterceptors()
	err = interceptors.RegistryInterceptor(authInter)
	if err != nil {
		logging.LogErrorF("failed to setup logging controller,err:%s", err.Error())
		return exception.WebServerErrEx2("failed to setup logging controller,error:%s", err.Error())
	}

	err = m.baseRouter.SetupControllerEx(webbase.HttpMethodGet|webbase.HttpMethodPost, basic.APIGroupRequire, urlFunMap, interceptors)
	if err != nil {
		logging.LogErrorF("failed to setup logging controller,err:%s", err.Error())
		return exception.WebServerErrEx2("failed to setup logging controller,error:%s", err.Error())
	}

	return nil
}

func (m *ServiceMain) SetupAgentController() error {
	agentService, err := service.GetAgentService(m.database)
	if err != nil {
		logging.LogErrorF("failed to create agent service,err:%s", err.Error())
		return exception.WebServerErrEx2("failed to create agent service,error:%s", err.Error())
	}
	agentController := selfController.GetAgentController(agentService)

	urlFunMap := make(map[string]gin.HandlerFunc)
	urlFunMap[cCommon.RelativeGetLLMProviders] = agentController.GetLLMProviders
	urlFunMap[cCommon.RelativeAddLLMProvider] = agentController.AddLLMProvider
	urlFunMap[cCommon.RelativeUpdateLLMProvider] = agentController.UpdateLLMProvider
	urlFunMap[cCommon.RelativeRemoveLLMProvider] = agentController.RemoveLLMProvider

	urlFunMap[cCommon.RelativeGetAgents] = agentController.GetAgents
	urlFunMap[cCommon.RelativeAddAgent] = agentController.AddAgent
	urlFunMap[cCommon.RelativeUpdateAgent] = agentController.UpdateAgent
	urlFunMap[cCommon.RelativeRemoveAgent] = agentController.RemoveAgent

	urlFunMap[cCommon.RelativeGetAgentStartupFiles] = agentController.GetStartupFiles
	urlFunMap[cCommon.RelativeGetAgentStartupFile] = agentController.GetStartupFile

	urlFunMap[cCommon.RelativeGetSkills] = agentController.GetSkills
	urlFunMap[cCommon.RelativeAddSkill] = agentController.AddSkill
	urlFunMap[cCommon.RelativeUpdateSkill] = agentController.UpdateSkill
	urlFunMap[cCommon.RelativeRemoveSkill] = agentController.RemoveSkill

	// 复用登录控制器创建的 APIGroupRequire 分组(其 DoFilter 已挂载认证 interceptor),
	// 因此本方法必须后于 SetupLoginController 调用,即可保证新 API 自动经过 token 校验。
	interceptors := interceptor.GetInterceptors()
	err = m.baseRouter.SetupControllerEx(webbase.HttpMethodGet|webbase.HttpMethodPost, basic.APIGroupRequire, urlFunMap, interceptors)
	if err != nil {
		logging.LogErrorF("failed to setup agent controller,err:%s", err.Error())
		return exception.WebServerErrEx2("failed to setup agent controller,error:%s", err.Error())
	}

	return nil
}

func (m *ServiceMain) SetupChatController() error {
	// function-calling 开关来自 ini [toolRule] functionCalling(默认关闭,安全兜底)
	functionCalling, _ := m.configHelper.GetBoolValueDefault("toolRule", "functionCalling", false)

	// 会话记录开关来自 ini [chat] sessionRecord(阶段1双写:只写不读的旁路,默认开启)
	sessionRecord, _ := m.configHelper.GetBoolValueDefault("chat", "sessionRecord", true)

	// 工具执行的配置规则,从配置文件加载
	toolRules := core2.LoadToolConfiguredRules(m.configHelper)

	chatService := service.GetChattingService(m.database, m.agentRepo, functionCalling, sessionRecord, toolRules)
	chatController := selfController.GetChatController(chatService, m.concurrent)

	urlFunMap := make(map[string]gin.HandlerFunc)
	urlFunMap[cCommon.RelativeChatUrl] = chatController.Chat
	urlFunMap[cCommon.RelativeChatStreamUrl] = chatController.ChatStream
	urlFunMap[cCommon.RelativeChatInterruptUrl] = chatController.InterruptChat
	urlFunMap[cCommon.RelativeGetSessions] = chatController.GetSessions
	urlFunMap[cCommon.RelativeGetSessionTurns] = chatController.GetSessionTurns

	// 复用登录控制器创建的 APIGroupRequire 分组(含认证 interceptor),
	// 因此本方法必须后于 SetupLoginController 调用。
	interceptors := interceptor.GetInterceptors()
	err := m.baseRouter.SetupControllerEx(webbase.HttpMethodPost, basic.APIGroupRequire, urlFunMap, interceptors)
	if err != nil {
		logging.LogErrorF("failed to setup chat controller,err:%s", err.Error())
		return exception.WebServerErrEx2("failed to setup chat controller,error:%s", err.Error())
	}

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
	// 启动失败（如端口被占用）时返回错误，由入口退出并执行 Destroy 清理资源，避免进程「假活」
	if !m.baseRouter.Startup(sslEnabled) {
		logging.LogErrorF("[Startup] web server failed to start")
		return exception.WebServerErrEx("web server start failed")
	}

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
	_ = concurrency.GetTaskManager().Close(nil)

	chat.GetTransportPool().Close()              // 彻底关闭所有链接
	concurrency.GetConcurrentAdvance().Destroy() //释放agent gate
	chat.GetToolsSearch().Destroy()              //释放工具仓库

	if m.agentRepo != nil {
		m.agentRepo.Destroy()
	}

	if m.agentDocCache != nil {
		m.agentDocCache.Destroy()
	}

	if m.agentFileRepo != nil {
		m.agentFileRepo.Destroy()
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

	// 这个一定要放在最后,退出前的日志还在缓冲区中
	logging.CloseLogger() // 日志最后刷盘
}

// ================================== private function & methods ==========================

func (m *ServiceMain) loadAgentConfigurations() error {
	// 1.首先判断所需的目录是否存在
	workSpaceDir := filepath.Join(pCommon.GetExecutableDir(), hCommon.AgentServiceWorkspaceDirName)
	_ = pCommon.CreateDir(workSpaceDir)

	// 2. 加载agent的默认配置信息
	configDir := filepath.Join(pCommon.GetExecutableDir(), "config")
	defaultAgentConfigFile := filepath.Join(configDir, hCommon.DefaultAgentConfigFile)
	m.defaultAgentConfigHelper, _ = config.GetConfigurationHelper(defaultAgentConfigFile)
	_ = m.defaultAgentConfigHelper.LoadConfiguration()

	// 3. load the agent & LLM providers configuration from the database
	if m.agentRepo == nil {
		m.agentRepo = repo.GetAgentRepository(m.database)
	}

	// 3.1 agent 配置缓存 TTL(多实例下各实例从 DB 刷新配置;单位秒,<=0 禁用)
	ttlSec, _ := m.configHelper.GetIntValueDefault("database", "reloadCacheTTL", cCommon.DefaultReloadDatabaseCacheTTLSeconds)
	agents := m.agentRepo.LoadAllAgents(int64(ttlSec))

	// 4. warm up the prompt repository with the default workspace (best-effort)
	if m.agentFileRepo == nil {
		m.agentFileRepo = ctxmgr.GetAgentFileRepository()
	}
	for _, agent := range agents {
		agentWorkspaceRoot, _ := hCommon.BuildAgentBootstrapRoot(agent)
		m.agentFileRepo.GetStartupFiles(agentWorkspaceRoot)
	}

	if m.agentDocCache == nil {
		m.agentDocCache = ctxmgr.GetAgentDocCache()
	}

	// 5.warm up the system tools when startup
	systemTools := m.agentRepo.GetSystemTools()
	_ = m.agentDocCache.LoadSystemTools(systemTools)

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
	loggingFilter := filter.GetLoggingFilter()        //NewLoggingFilter(basic.FilterOrderLogging)
	reqPreprocessFilter := filter.GetRecoveryFilter() //NewRequestPreprocessFilter(basic.FilterOrderReqPre)
	addressFilter := filter.GetAddressFilter()        //NewAddressFilter(basic.FilterOrderAddress)
	tempStr, _ := m.configHelper.GetStringValueDefault("security", "allowedUrl", "")
	urls := strings.Split(tempStr, ",")
	addressFilter.SetupAllowedUrls(urls)

	trafficFilter := filter.GetTrafficFilter(m.memoryDB) //NewTrafficFilter(basic.FilterOrderTraffic, m.memoryDB)
	// 免除流量控制的 URL(如交互式聊天流接口,避免被 IP/IP+API 频率上限在长会话中误伤)
	tempStr, _ = m.configHelper.GetStringValueDefault("security", "unTrafficUrl", "")
	urls = strings.Split(tempStr, ",")
	trafficFilter.SetupUnTrafficUrl(urls)

	securityFilter := filter.GetSecurityFilter(m.memoryDB, signKey) //NewSecurityFilter(basic.FilterOrderSecurity, m.memoryDB, signKey)
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
