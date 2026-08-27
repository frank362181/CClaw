/**
* Agent 管理控制器(LLM Provider / Agent / Skill 的列表、新增、更新、删除)
*
* 安装:ServiceMain.SetupAgentController()。路由挂载于 /api 分组,自动经过
* 全局 filter(地址/安全/流量等)与认证 interceptor(需携带 token)。
**/

package controller

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	cclawcommon "my.favorite/cclawservice/common"
	model2 "my.favorite/cclawservice/controller/model"
	exception2 "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/service"
	"my.favorite/cclawservice/utility"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
	"my.favorite/platformutility/webbase/middleware/controller"
	baseController "my.favorite/platformutility/webbase/middleware/controller"
	pService "my.favorite/platformutility/webbase/middleware/service"
)

type AgentController struct {
	baseController.BaseController
}

var agentController = common.GetSingleton(func() *AgentController {
	return &AgentController{
		BaseController: controller.BaseController{
			Type:    basic.APIGroupRequire,
			Name:    "agentController",
			Service: nil,
		},
	}
}).Instance()

func GetAgentController(svc pService.IService) *AgentController {
	agentController.Service = svc
	return agentController
}

// ============================== LLM Provider =============================

// GetLLMProviders 获取 LLM Provider 列表
//
//	@Summary		获取LLM Provider列表
//	@Description	获取全部未删除的LLM Provider列表
//	@Tags			Agent管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string	true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string	true	"盐"
//	@Param			TIMESTAMP		query		string	true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string	true	"对请求内容的签名秒数"
//	@Success		200				{object}	basic.CommonResponse
//	@Failure		400				{object}	basic.CommonResponse
//	@Router			/api/llmProviders [get]
func (ac *AgentController) GetLLMProviders(c *gin.Context) {
	svc, ok := ac.getAgentService(c)
	if !ok {
		return
	}

	loginSession, ok := utility.GetAuthentication(c)
	if !ok {
		return
	}

	list, err := svc.GetLLMProviders(loginSession.ComCode)
	if err != nil {
		ac.respondAgentError(c, err)
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, list)
}

// AddLLMProvider 新增 LLM Provider
//
//	@Summary		新增LLM Provider
//	@Description	新增一个LLM Provider,ProviderId 重复时返回409
//	@Tags			Agent管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string					true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string					true	"盐"
//	@Param			TIMESTAMP		query		string					true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string					true	"对请求内容的签名秒数"
//	@Param			body			body		dataobject.TLLMProvider	true	"LLM Provider 对象"
//	@Success		200				{object}	basic.CommonResponse
//	@Failure		400				{object}	basic.CommonResponse
//	@Router			/api/llmProvider/add [post]
func (ac *AgentController) AddLLMProvider(c *gin.Context) {
	svc, ok := ac.getAgentService(c)
	if !ok {
		return
	}

	loginSession, ok := utility.GetAuthentication(c) //ac.isAuthenticated(c)
	if !ok {
		return
	}

	var p dataobject.TLlm
	if err := c.ShouldBindBodyWithJSON(&p); err != nil {
		basic.GetBaseResponser().Failure(c, http.StatusBadRequest, cclawcommon.ConstErrorAgentParameter, cclawcommon.ConstErrorAgentParameterMsg, nil)
		return
	}
	p.OwnerId = loginSession.ComCode
	if err := svc.AddLLMProvider(p); err != nil {
		ac.respondAgentError(c, err)
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, nil)
}

// UpdateLLMProvider 更新 LLM Provider
//
//	@Summary		更新LLM Provider
//	@Description	更新LLM Provider,需携带 dVersion(乐观锁),版本冲突时返回409
//	@Tags			Agent管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string					true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string					true	"盐"
//	@Param			TIMESTAMP		query		string					true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string					true	"对请求内容的签名秒数"
//	@Param			body			body		dataobject.TLLMProvider	true	"LLM Provider 对象(含 dVersion)"
//	@Success		200				{object}	basic.CommonResponse
//	@Failure		400				{object}	basic.CommonResponse
//	@Router			/api/llmProvider/update [post]
func (ac *AgentController) UpdateLLMProvider(c *gin.Context) {
	svc, ok := ac.getAgentService(c)
	if !ok {
		return
	}

	loginSession, ok := utility.GetAuthentication(c) //ac.isAuthenticated(c)
	if !ok {
		return
	}

	var p dataobject.TLlm
	if err := c.ShouldBindBodyWithJSON(&p); err != nil {
		basic.GetBaseResponser().Failure(c, http.StatusBadRequest, cclawcommon.ConstErrorAgentParameter, cclawcommon.ConstErrorAgentParameterMsg, nil)
		return
	}

	p.OwnerId = loginSession.ComCode
	if err := svc.UpdateLLMProvider(p); err != nil {
		ac.respondAgentError(c, err)
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, nil)
}

// RemoveLLMProvider 删除 LLM Provider(软删除)
//
//	@Summary		删除LLM Provider
//	@Description	软删除LLM Provider,记录不存在时返回404
//	@Tags			Agent管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string							true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string							true	"盐"
//	@Param			TIMESTAMP		query		string							true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string							true	"对请求内容的签名秒数"
//	@Param			body			body		model.LLMProviderIDRequest		true	"ProviderId"
//	@Success		200				{object}	basic.CommonResponse
//	@Failure		400				{object}	basic.CommonResponse
//	@Router			/api/llmProvider/remove [post]
func (ac *AgentController) RemoveLLMProvider(c *gin.Context) {
	svc, ok := ac.getAgentService(c)
	if !ok {
		return
	}

	loginSession, ok := utility.GetAuthentication(c) //ac.isAuthenticated(c)
	if !ok {
		return
	}

	var req model2.LLMProviderIDRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		basic.GetBaseResponser().Failure(c, http.StatusBadRequest, cclawcommon.ConstErrorAgentParameter, cclawcommon.ConstErrorAgentParameterMsg, nil)
		return
	}

	if err := svc.RemoveLLMProvider(loginSession.ComCode, req.ProviderId); err != nil {
		ac.respondAgentError(c, err)
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, nil)
}

// ============================== Agent =============================

// GetAgents 获取 Agent 列表
//
//	@Summary		获取Agent列表
//	@Description	获取全部未删除的Agent列表
//	@Tags			Agent管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string	true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string	true	"盐"
//	@Param			TIMESTAMP		query		string	true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string	true	"对请求内容的签名秒数"
//	@Success		200				{object}	basic.CommonResponse
//	@Failure		400				{object}	basic.CommonResponse
//	@Router			/api/agents [get]
func (ac *AgentController) GetAgents(c *gin.Context) {
	svc, ok := ac.getAgentService(c)
	if !ok {
		return
	}

	loginSession, ok := utility.GetAuthentication(c) //ac.isAuthenticated(c)
	if !ok {
		return
	}

	list, err := svc.GetAgents(loginSession.ComCode)
	if err != nil {
		ac.respondAgentError(c, err)
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, list)
}

// AddAgent 新增 Agent
//
//	@Summary		新增Agent
//	@Description	新增一个Agent,AgentId 重复时返回409
//	@Tags			Agent管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string			true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string			true	"盐"
//	@Param			TIMESTAMP		query		string			true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string			true	"对请求内容的签名秒数"
//	@Param			body			body		dataobject.TAgent	true	"Agent 对象"
//	@Success		200				{object}	basic.CommonResponse
//	@Failure		400				{object}	basic.CommonResponse
//	@Router			/api/agent/add [post]
func (ac *AgentController) AddAgent(c *gin.Context) {
	svc, ok := ac.getAgentService(c)
	if !ok {
		return
	}

	loginSession, ok := utility.GetAuthentication(c) //ac.isAuthenticated(c)
	if !ok {
		return
	}

	var a dataobject.TAgent
	if err := c.ShouldBindBodyWithJSON(&a); err != nil {
		basic.GetBaseResponser().Failure(c, http.StatusBadRequest, cclawcommon.ConstErrorAgentParameter, cclawcommon.ConstErrorAgentParameterMsg, nil)
		return
	}

	a.OwnerId = loginSession.ComCode
	if err := svc.AddAgent(a); err != nil {
		ac.respondAgentError(c, err)
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, nil)
}

// UpdateAgent 更新 Agent
//
//	@Summary		更新Agent
//	@Description	更新Agent,需携带 dVersion(乐观锁),版本冲突时返回409
//	@Tags			Agent管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string			true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string			true	"盐"
//	@Param			TIMESTAMP		query		string			true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string			true	"对请求内容的签名秒数"
//	@Param			body			body		dataobject.TAgent	true	"Agent 对象(含 dVersion)"
//	@Success		200				{object}	basic.CommonResponse
//	@Failure		400				{object}	basic.CommonResponse
//	@Router			/api/agent/update [post]
func (ac *AgentController) UpdateAgent(c *gin.Context) {
	svc, ok := ac.getAgentService(c)
	if !ok {
		return
	}

	loginSession, ok := utility.GetAuthentication(c) //ac.isAuthenticated(c)
	if !ok {
		return
	}

	var a dataobject.TAgent
	if err := c.ShouldBindBodyWithJSON(&a); err != nil {
		basic.GetBaseResponser().Failure(c, http.StatusBadRequest, cclawcommon.ConstErrorAgentParameter, cclawcommon.ConstErrorAgentParameterMsg, nil)
		return
	}

	a.OwnerId = loginSession.ComCode
	if err := svc.UpdateAgent(a); err != nil {
		ac.respondAgentError(c, err)
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, nil)
}

// RemoveAgent 删除 Agent(软删除)
//
//	@Summary		删除Agent
//	@Description	软删除Agent,记录不存在时返回404
//	@Tags			Agent管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string					true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string					true	"盐"
//	@Param			TIMESTAMP		query		string					true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string					true	"对请求内容的签名秒数"
//	@Param			body			body		model.AgentIDRequest	true	"AgentId"
//	@Success		200				{object}	basic.CommonResponse
//	@Failure		400				{object}	basic.CommonResponse
//	@Router			/api/agent/remove [post]
func (ac *AgentController) RemoveAgent(c *gin.Context) {
	svc, ok := ac.getAgentService(c)
	if !ok {
		return
	}

	loginSession, ok := utility.GetAuthentication(c) //ac.isAuthenticated(c)
	if !ok {
		return
	}
	var req model2.AgentIDRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		basic.GetBaseResponser().Failure(c, http.StatusBadRequest, cclawcommon.ConstErrorAgentParameter, cclawcommon.ConstErrorAgentParameterMsg, nil)
		return
	}

	if err := svc.RemoveAgent(loginSession.ComCode, req.AgentId); err != nil {
		ac.respondAgentError(c, err)
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, nil)
}

// ============================== Agent Startup Files =============================

// GetStartupFiles 获取某 Agent 的启动文件列表
//
//	@Summary		获取Agent启动文件列表
//	@Description	返回指定 Agent 的全部启动文件(AGENTS.md/SOUL.md/...),仅元数据,不含文件内容
//	@Tags			Agent管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string				true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string				true	"盐"
//	@Param			TIMESTAMP		query		string				true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string				true	"对请求内容的签名秒数"
//	@Param			body			body		model.AgentIDRequest	true	"AgentId"
//	@Success		200				{object}	basic.CommonResponse
//	@Failure		400				{object}	basic.CommonResponse
//	@Router			/api/agent/startupFiles [post]
func (ac *AgentController) GetStartupFiles(c *gin.Context) {
	svc, ok := ac.getAgentService(c)
	if !ok {
		return
	}

	loginSession, ok := utility.GetAuthentication(c) //ac.isAuthenticated(c)
	if !ok {
		return
	}

	var req model2.AgentIDRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		basic.GetBaseResponser().Failure(c, http.StatusBadRequest, cclawcommon.ConstErrorAgentParameter, cclawcommon.ConstErrorAgentParameterMsg, nil)
		return
	}

	list, err := svc.GetAgentStartupFiles(loginSession.ComCode, req.AgentId)
	if err != nil {
		ac.respondAgentError(c, err)
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, list)
}

// GetStartupFile 获取某 Agent 指定启动文件的内容
//
//	@Summary		获取Agent启动文件内容
//	@Description	返回指定 Agent 指定启动文件(须命中启动文件白名单)的原始内容
//	@Tags			Agent管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string						true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string						true	"盐"
//	@Param			TIMESTAMP		query		string						true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string						true	"对请求内容的签名秒数"
//	@Param			body			body		model.AgentStartupFileRequest	true	"AgentId + 启动文件名"
//	@Success		200				{object}	basic.CommonResponse
//	@Failure		400				{object}	basic.CommonResponse
//	@Router			/api/agent/startupFile [post]
func (ac *AgentController) GetStartupFile(c *gin.Context) {
	svc, ok := ac.getAgentService(c)
	if !ok {
		return
	}

	loginSession, ok := utility.GetAuthentication(c) //ac.isAuthenticated(c)
	if !ok {
		return
	}

	var req model2.AgentStartupFileRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		basic.GetBaseResponser().Failure(c, http.StatusBadRequest, cclawcommon.ConstErrorAgentParameter, cclawcommon.ConstErrorAgentParameterMsg, nil)
		return
	}

	file, err := svc.GetAgentStartupFile(loginSession.ComCode, req.AgentId, req.FileName)
	if err != nil {
		ac.respondAgentError(c, err)
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, file)
}

// ============================== Skill =============================

// GetSkills 获取 Skill 列表
//
//	@Summary		获取Skill列表
//	@Description	获取全部未删除的Skill列表
//	@Tags			Agent管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string	true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string	true	"盐"
//	@Param			TIMESTAMP		query		string	true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string	true	"对请求内容的签名秒数"
//	@Success		200				{object}	basic.CommonResponse
//	@Failure		400				{object}	basic.CommonResponse
//	@Router			/api/skills [get]
func (ac *AgentController) GetSkills(c *gin.Context) {
	svc, ok := ac.getAgentService(c)
	if !ok {
		return
	}

	loginSession, ok := utility.GetAuthentication(c) //ac.isAuthenticated(c)
	if !ok {
		return
	}

	list, err := svc.GetSkills(loginSession.ComCode)
	if err != nil {
		ac.respondAgentError(c, err)
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, list)
}

// AddSkill 新增 Skill
//
//	@Summary		新增Skill
//	@Description	新增一个Skill,SkillId 重复时返回409
//	@Tags			Agent管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string			true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string			true	"盐"
//	@Param			TIMESTAMP		query		string			true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string			true	"对请求内容的签名秒数"
//	@Param			body			body		dataobject.TSkill	true	"Skill 对象"
//	@Success		200				{object}	basic.CommonResponse
//	@Failure		400				{object}	basic.CommonResponse
//	@Router			/api/skill/add [post]
func (ac *AgentController) AddSkill(c *gin.Context) {
	svc, ok := ac.getAgentService(c)
	if !ok {
		return
	}

	loginSession, ok := utility.GetAuthentication(c) //ac.isAuthenticated(c)
	if !ok {
		return
	}

	var sk dataobject.TSkill
	if err := c.ShouldBindBodyWithJSON(&sk); err != nil {
		basic.GetBaseResponser().Failure(c, http.StatusBadRequest, cclawcommon.ConstErrorAgentParameter, cclawcommon.ConstErrorAgentParameterMsg, nil)
		return
	}

	sk.OwnerId = loginSession.ComCode
	if err := svc.AddSkill(sk); err != nil {
		ac.respondAgentError(c, err)
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, nil)
}

// UpdateSkill 更新 Skill
//
//	@Summary		更新Skill
//	@Description	更新Skill,需携带 dVersion(乐观锁),版本冲突时返回409
//	@Tags			Agent管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string			true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string			true	"盐"
//	@Param			TIMESTAMP		query		string			true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string			true	"对请求内容的签名秒数"
//	@Param			body			body		dataobject.TSkill	true	"Skill 对象(含 dVersion)"
//	@Success		200				{object}	basic.CommonResponse
//	@Failure		400				{object}	basic.CommonResponse
//	@Router			/api/skill/update [post]
func (ac *AgentController) UpdateSkill(c *gin.Context) {
	svc, ok := ac.getAgentService(c)
	if !ok {
		return
	}

	loginSession, ok := utility.GetAuthentication(c) //ac.isAuthenticated(c)
	if !ok {
		return
	}

	var sk dataobject.TSkill
	if err := c.ShouldBindBodyWithJSON(&sk); err != nil {
		basic.GetBaseResponser().Failure(c, http.StatusBadRequest, cclawcommon.ConstErrorAgentParameter, cclawcommon.ConstErrorAgentParameterMsg, nil)
		return
	}

	sk.OwnerId = loginSession.ComCode
	if err := svc.UpdateSkill(sk); err != nil {
		ac.respondAgentError(c, err)
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, nil)
}

// RemoveSkill 删除 Skill(软删除)
//
//	@Summary		删除Skill
//	@Description	软删除Skill,记录不存在时返回404
//	@Tags			Agent管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string					true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string					true	"盐"
//	@Param			TIMESTAMP		query		string					true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string					true	"对请求内容的签名秒数"
//	@Param			body			body		model.SkillIDRequest	true	"SkillId"
//	@Success		200				{object}	basic.CommonResponse
//	@Failure		400				{object}	basic.CommonResponse
//	@Router			/api/skill/remove [post]
func (ac *AgentController) RemoveSkill(c *gin.Context) {
	svc, ok := ac.getAgentService(c)
	if !ok {
		return
	}

	loginSession, ok := utility.GetAuthentication(c) //ac.isAuthenticated(c)
	if !ok {
		return
	}
	var req model2.SkillIDRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		basic.GetBaseResponser().Failure(c, http.StatusBadRequest, cclawcommon.ConstErrorAgentParameter, cclawcommon.ConstErrorAgentParameterMsg, nil)
		return
	}

	if err := svc.RemoveSkill(loginSession.ComCode, req.SkillId); err != nil {
		ac.respondAgentError(c, err)
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, nil)
}

// =========================== private functions & methods ================================
// getAgentService 获取当前注入的 AgentService
func (ac *AgentController) getAgentService(c *gin.Context) (*service.AgentService, bool) {
	svc, ok := ac.GetService().(*service.AgentService)
	if !ok || svc == nil {
		//内部服务器错误
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
		return nil, false
	}
	return svc, true
}

// respondAgentError 业务层的错误表现为http正常,但业务层异常
func (ac *AgentController) respondAgentError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, exception2.AgentParameterErr):
		basic.GetBaseResponser().Failure(c, http.StatusOK, cclawcommon.ConstErrorAgentParameter, cclawcommon.ConstErrorAgentParameterMsg, nil)
	case errors.Is(err, exception2.AgentNotFoundErr):
		basic.GetBaseResponser().Failure(c, http.StatusOK, cclawcommon.ConstErrorAgentNotFound, cclawcommon.ConstErrorAgentNotFoundMsg, nil)
	case errors.Is(err, exception2.AgentWorkspaceErr):
		// 智能体工作区目录缺失/非法:与"记录不存在"区分,便于前端给准确提示
		basic.GetBaseResponser().Failure(c, http.StatusOK, cclawcommon.ConstErrorAgentWorkspace, cclawcommon.ConstErrorAgentWorkspaceMsg, nil)
	case errors.Is(err, exception2.AgentConflictErr):
		basic.GetBaseResponser().Failure(c, http.StatusOK, cclawcommon.ConstErrorAgentConflict, cclawcommon.ConstErrorAgentConflictMsg, nil)
	case errors.Is(err, exception2.AgentInternalErr):
		// 服务/仓库未初始化等内部错误
		basic.GetBaseResponser().Failure(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
	default:
		basic.GetBaseResponser().Failure(c, http.StatusOK, basic.ConstErrorUnknow, basic.ConstErrorUnknowMsg, nil)
	}
}
