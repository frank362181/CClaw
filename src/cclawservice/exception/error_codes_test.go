/**
* 业务错误的契约测试(agent / login / database / harness / chat)。
*
* 锁定的是**对外契约**:业务码绝对数值(前端按 code 分支)、HTTP 状态、用户文案非空,
* 以及兼容层(旧哨兵/旧工厂)与 Kind 判定等价。
* 注:业务码"重复"由 apperr.Register 在包初始化期直接 panic 拦截,测试无法覆盖(panic 先于测试)。
 */

package exception

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"my.favorite/cclawservice/apperr"
	"my.favorite/cclawservice/llmproto"
	"my.favorite/platformutility/webbase/middleware/basic"
)

// 业务码基线:platformutility basic.ConstErrorMax 的当前取值(平台已改为
// ConstSuccessCode + 2000,不再依赖声明位置)。若它变化,所有外部业务码会整体偏移,
// 必须显式重新确认 —— 这就是这里断言绝对数值的原因。
const wantConstErrorMax = 2000

// 各域基址(与 exception/error_*.go 中的 code*Base 一致),域内自 +1 起连续分配。
const (
	wantUserBase     = wantConstErrorMax + 1000 // user/login 域 → 3001..
	wantDatabaseBase = wantConstErrorMax + 1400 // database 域  → 3401
	wantAgentBase    = wantConstErrorMax + 2000 // agent 域     → 4001..
	wantHarnessBase  = wantConstErrorMax + 2500 // harness.runtime_client → 4501
	wantChatBase     = wantConstErrorMax + 2600 // 聊天域 → 4601..
)

// TestErrorCodeContract 锁定每个 Kind 的名称 / 业务码 / HTTP 状态 / 文案非空。
func TestErrorCodeContract(t *testing.T) {
	if basic.ConstErrorMax != wantConstErrorMax {
		t.Fatalf("平台基础业务码漂移: basic.ConstErrorMax=%d, 期望 %d(外部业务码会整体偏移)", basic.ConstErrorMax, wantConstErrorMax)
	}

	cases := []struct {
		name       string
		kind       *apperr.Kind
		code       int
		httpStatus int
	}{
		// user/login 域:3001..3005
		{"login.un_authorized", UserUnAuthorizedKind, wantUserBase + 1, http.StatusUnauthorized},
		{"login.parameter", LoginParameterKind, wantUserBase + 2, http.StatusBadRequest},
		{"login.user_password_unmatch", UserPasswordUnmatchKind, wantUserBase + 3, http.StatusOK},
		{"login.non_token", NonTokenKind, wantUserBase + 4, http.StatusUnauthorized},
		{"login.invalid_token", InvalidTokenKind, wantUserBase + 5, http.StatusUnauthorized},
		// database 域:3401
		{"database.query", DatabaseQueryKind, wantDatabaseBase + 1, http.StatusInternalServerError},
		// harness 调用侧:4501
		{"harness.runtime_client", RuntimeClientKind, wantHarnessBase + 1, http.StatusInternalServerError},
		// agent 域:4001..4004(业务失败沿用 200 + 业务码的既有约定)
		{"agent.parameter", AgentParameterKind, wantAgentBase + 1, http.StatusOK},
		{"agent.not_found", AgentNotFoundKind, wantAgentBase + 2, http.StatusOK},
		{"agent.conflict", AgentConflictKind, wantAgentBase + 3, http.StatusOK},
		{"agent.workspace", AgentWorkspaceKind, wantAgentBase + 4, http.StatusOK},
		// agent.internal 不对外暴露业务码(Code=0),响应由 controller 回退平台通用内部错误码。
		{"agent.internal", AgentInternalKind, 0, http.StatusInternalServerError},
		// 聊天域:4601..4616(独立 4600 段,自 +1 起)
		{"chat.parameter", ChatParameterKind, wantChatBase + 1, http.StatusBadRequest},
		{"chat.agent_not_found", ChatAgentNotFoundKind, wantChatBase + 2, http.StatusNotFound},
		{"chat.llm", ChatLLMKind, wantChatBase + 3, http.StatusBadGateway},
		{"chat.canceled", ChatCanceledKind, wantChatBase + 4, 0},
		{"chat.not_active", ChatNotActiveKind, wantChatBase + 5, http.StatusBadRequest},
		{"chat.busy", ChatBusyKind, wantChatBase + 6, http.StatusServiceUnavailable},
		{"chat.server_busy", ChatServerBusyKind, wantChatBase + 7, http.StatusServiceUnavailable},
		{"chat.tool_loop", ChatToolLoopKind, wantChatBase + 8, http.StatusBadGateway},
		{"chat.llm.quota", ChatLLMQuotaKind, wantChatBase + 9, http.StatusBadGateway},
		{"chat.run_timeout", ChatRunTimeoutKind, wantChatBase + 10, http.StatusGatewayTimeout},
		{"chat.llm.auth", ChatLLMAuthKind, wantChatBase + 11, http.StatusBadGateway},
		{"chat.llm.forbidden", ChatLLMForbiddenKind, wantChatBase + 12, http.StatusBadGateway},
		{"chat.llm.not_found", ChatLLMNotFoundKind, wantChatBase + 13, http.StatusBadGateway},
		{"chat.llm.invalid_request", ChatLLMInvalidRequestKind, wantChatBase + 14, http.StatusBadGateway},
		{"chat.llm.rate_limit", ChatLLMRateLimitKind, wantChatBase + 15, http.StatusTooManyRequests},
		{"chat.llm.content_filter", ChatLLMContentFilterKind, wantChatBase + 16, http.StatusBadGateway},
	}

	for _, c := range cases {
		if c.kind.Name != c.name {
			t.Errorf("Name 不一致: 期望 %s, 实际 %s", c.name, c.kind.Name)
		}
		if c.kind.Code != c.code {
			t.Errorf("%s: 业务码不一致: 期望 %d, 实际 %d", c.name, c.code, c.kind.Code)
		}
		if c.kind.HTTPStatus != c.httpStatus {
			t.Errorf("%s: HTTP 状态不一致: 期望 %d, 实际 %d", c.name, c.httpStatus, c.kind.HTTPStatus)
		}
		if c.kind.Msg == "" {
			t.Errorf("%s: 用户文案为空", c.name)
		}
		if got := apperr.LookupByName(c.name); got != c.kind {
			t.Errorf("%s: 注册表反查失败", c.name)
		}
	}
}

// TestLegacyWrapperStillWorks 兼容层:旧哨兵与旧工厂仍可用,且与 Kind 判定等价。
// (调用点无需改动,错误已纳入 apperr 统一管理。)
func TestLegacyWrapperStillWorks(t *testing.T) {
	cases := []struct {
		name     string
		sentinel error
		kind     *apperr.Kind
		build    func(string) error
	}{
		{"agent.workspace", AgentWorkspaceErr, AgentWorkspaceKind, func(s string) error { return AgentWorkspaceErrEx(s) }},
		{"agent.internal", AgentInternalErr, AgentInternalKind, func(s string) error { return AgentInternalErrEx(s) }},
		{"login.user_password_unmatch", UserPasswordUnmatchErr, UserPasswordUnmatchKind, func(s string) error { return UserPasswordUnmatchEx(s) }},
		{"login.invalid_token", TokenInvalidErr, InvalidTokenKind, func(s string) error { return TokenInvalidErrEx(s) }},
		{"database.query", DatabaseQueryErr, DatabaseQueryKind, func(s string) error { return DatabaseQueryErrEx(s) }},
		{"harness.runtime_client", RuntimeClientErr, RuntimeClientKind, func(s string) error { return RuntimeClientErrEx(s) }},
	}

	for _, c := range cases {
		err := c.build("detail-" + c.name)
		if apperr.Of(err) != c.kind {
			t.Errorf("%s: Kind 不一致: %v", c.name, apperr.Of(err))
		}
		if !errors.Is(err, c.kind) {
			t.Errorf("%s: errors.Is(Kind) 应为真", c.name)
		}
		if !errors.Is(err, c.sentinel) {
			t.Errorf("%s: errors.Is(旧哨兵) 应为真", c.name)
		}
		if !strings.Contains(err.Error(), "detail-"+c.name) {
			t.Errorf("%s: 细节丢失: %s", c.name, err.Error())
		}
	}

	// 工厂应保留格式化参数与产生位置(file:line),便于日志排障
	err := AgentWorkspaceErrEx2("agent %s 目录缺失", "a1")
	if !strings.Contains(err.Error(), "a1") {
		t.Errorf("格式化细节丢失: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "error_agent") {
		t.Errorf("产生位置丢失: %s", err.Error())
	}
}

// TestErrRequestCanceledIsSentinel 取消哨兵保持独立(不属于任何业务 Kind;已改由 llmproto 提供)。
func TestErrRequestCanceledIsSentinel(t *testing.T) {
	if !errors.Is(llmproto.ErrRequestCanceled, llmproto.ErrRequestCanceled) {
		t.Fatalf("哨兵应可自比较")
	}
	if apperr.Of(llmproto.ErrRequestCanceled) != nil {
		t.Fatalf("取消哨兵不应被识别为应用错误")
	}
}

// TestNotifyContract "必须显式提醒用户"的错误固定为这批(用户能自行处理的额度/配置/合规/规模类):
// 它们在 SSE 上会被额外下发一条 error 级告警(chat_error),即使回答已输出过内容也不得降级。
// 其余错误只走普通提示,避免告警噪音。
func TestNotifyContract(t *testing.T) {
	mustNotify := []*apperr.Kind{
		ChatLLMQuotaKind,          // 欠费/额度不足:需充值或提升配额
		ChatLLMAuthKind,           // API Key 无效/未配置:需改配置
		ChatLLMForbiddenKind,      // 未开通/未授权/模型下线:需开通或授权
		ChatLLMNotFoundKind,       // 模型名配置错误:需改配置
		ChatLLMInvalidRequestKind, // 参数/上下文不合法:需改配置
		ChatLLMContentFilterKind,  // 内容不合规:需改输入
		ChatRunTimeoutKind,        // 任务超时:需缩小范围
		ChatToolLoopKind,          // 步骤过多:需缩小范围
		ChatBusyKind,              // 目标 Agent 繁忙:稍后重试
		ChatServerBusyKind,        // 实例整体繁忙:稍后重试
	}
	for _, kind := range mustNotify {
		if !kind.Notify {
			t.Errorf("%s 必须标记 Notify(用户可自行处理,需在 UI 显式提醒)", kind.Name)
		}
		if kind.Silent {
			t.Errorf("%s 不能同时 Silent(注册期应已拦截)", kind.Name)
		}
	}

	plain := []*apperr.Kind{
		ChatCanceledKind,     // 客户端已断开/主动中断:静默
		ChatParameterKind,    // 请求参数错误:前端契约问题,提醒用户无意义
		ChatLLMKind,          // 未归类的引擎故障:走普通提示
		ChatLLMRateLimitKind, // 限流:客户端已自动重试,避免告警噪音
	}
	for _, kind := range plain {
		if kind.Notify {
			t.Errorf("%s 不应标记 Notify(会造成告警噪音)", kind.Name)
		}
	}
}
