/*
* agent 管理(LLM Provider/Agent/Skill)的业务错误定义(单一事实来源)。
*
* 每类错误只声明一行 Kind 并注册到 apperr 统一管理:名称 / 对前端的业务码 / HTTP 状态 /
* 用户文案集中在一处,controller 不再各自维护"错误 → 状态码 / 文案"的映射表。
*
 * 业务码为前端契约(前端按 code 分支):基址 ConstErrorMax + 2000 = 4000,
 * 域内自 +1 起按声明顺序连续分配(4001..4004),数值由 error_codes_test.go 锁定。
*
* 兼容层:AgentWorkspaceErr/AgentInternalErr 及其 Ex/Ex2 工厂保持原签名不变,
* 既有调用点无需改动;它们只是 Kind 的薄封装。
*/

package exception

import (
	"net/http"

	"my.favorite/cclawservice/apperr"
	"my.favorite/platformutility/webbase/middleware/basic"
)

const (
	codeAgentParameter = codeErrorAgentBase + 1 + iota // 参数错误
	codeAgentNotFound                                  // 智能体不存在
	codeAgentConflict                                  // 智能体已存在/版本冲突
	codeAgentWorkspace                                 // 工作空间不可用
)

var (
	// AgentParameterKind agent 参数错误(请求体/字段不合法)。
	// agent 域历史上以 200 + 业务码表达业务失败,故 HTTPStatus 记为 200(契约不变)。
	AgentParameterKind = apperr.Register(apperr.Kind{
		Name:       "agent.parameter",
		Code:       codeAgentParameter,
		HTTPStatus: http.StatusOK,
		Msg:        "参数错误",
	})

	// AgentNotFoundKind 目标智能体不存在。
	AgentNotFoundKind = apperr.Register(apperr.Kind{
		Name:       "agent.not_found",
		Code:       codeAgentNotFound,
		HTTPStatus: http.StatusOK,
		Msg:        "智能体不存在或已被删除",
	})

	// AgentConflictKind 智能体已存在或版本冲突。
	AgentConflictKind = apperr.Register(apperr.Kind{
		Name:       "agent.conflict",
		Code:       codeAgentConflict,
		HTTPStatus: http.StatusOK,
		Msg:        "智能体已存在或版本冲突",
	})

	// AgentWorkspaceKind 智能体工作空间不可用(目录缺失或 agent_dir 非法)。
	// 与"记录不存在"区分,便于前端给准确提示。
	AgentWorkspaceKind = apperr.Register(apperr.Kind{
		Name:       "agent.workspace",
		Code:       codeAgentWorkspace,
		HTTPStatus: http.StatusOK,
		Msg:        "智能体工作空间不可用",
	})

	// AgentInternalKind 服务/仓库未初始化等内部错误。
	// 内部错误不对外暴露业务码(Code=0),响应回退平台通用 500 码与文案。
	AgentInternalKind = apperr.Register(apperr.Kind{
		Name:       "agent.internal",
		HTTPStatus: http.StatusInternalServerError,
		Msg:        basic.ConstInternalServerErrorMsg,
	})
)

// ===== 兼容层:保持既有标识符与工厂签名,调用点无需改动 =====

// AgentWorkspaceErr 工作空间错误的哨兵(errors.Is 用)。
var AgentWorkspaceErr = AgentWorkspaceKind.Sentinel()

func AgentWorkspaceErrEx(errTxt string) *apperr.Error {
	return AgentWorkspaceKind.New(errTxt)
}

func AgentWorkspaceErrEx2(errTxt string, a ...interface{}) *apperr.Error {
	return AgentWorkspaceKind.Newf(errTxt, a...)
}

// AgentInternalErr 内部错误的哨兵(errors.Is 用)。
var AgentInternalErr = AgentInternalKind.Sentinel()

func AgentInternalErrEx(errTxt string) *apperr.Error {
	return AgentInternalKind.New(errTxt)
}

func AgentInternalErrEx2(errTxt string, a ...interface{}) *apperr.Error {
	return AgentInternalKind.Newf(errTxt, a...)
}
