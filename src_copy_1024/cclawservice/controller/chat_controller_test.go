/**
* 聊天错误响应的 HTTP / 业务码契约测试(controller 层)
*
* 为什么在这里补测:业务码与 HTTP 状态是**对外契约**(前端按 code 分支、按 notify 决定是否用醒目样式),
* 由 Kind 定义 + respondChatError 共同决定 —— 只断言 Kind 常量不够,要断言真正写进响应体的内容。
**/

package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"my.favorite/cclawservice/apperr"
	cCommon "my.favorite/cclawservice/common"
	myException "my.favorite/cclawservice/exception"
	"my.favorite/platformutility/webbase/middleware/basic"
)

// newControllerTestContext 构造 gin 测试上下文与响应记录器。
func newControllerTestContext(t *testing.T) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	return recorder, ctx
}

// TestRespondChatError_ToolLoopGuard 验证工具死循环守卫的终止错误按 Kind 渲染:
// HTTP 502 + 业务码 4617 + Kind 文案,且带 notify(前端据此用醒目样式提示用户可自行处理)。
func TestRespondChatError_ToolLoopGuard(t *testing.T) {
	recorder, ctx := newControllerTestContext(t)

	err := myException.ChatToolLoopGuardKind.Newf("chat service: tool loop: %s", "stopped by loop guard")
	if !apperr.Notify(err) {
		t.Error("守卫终止错误应带 notify(前端据此提示用户), Kind 定义变更需同步确认")
	}

	respondChatError(ctx, err)

	if recorder.Code != http.StatusBadGateway {
		t.Errorf("HTTP 状态 = %d, want %d", recorder.Code, http.StatusBadGateway)
	}

	var resp basic.CommonResponse
	if e := json.Unmarshal(recorder.Body.Bytes(), &resp); e != nil {
		t.Fatalf("响应体应为合法 JSON: %v (body=%s)", e, recorder.Body.String())
	}
	if resp.RespCode != myException.ChatToolLoopGuardKind.Code {
		t.Errorf("业务码 = %d, want %d", resp.RespCode, myException.ChatToolLoopGuardKind.Code)
	}
	if resp.RespMsg != myException.ChatToolLoopGuardKind.Msg {
		t.Errorf("用户文案 = %q, want %q", resp.RespMsg, myException.ChatToolLoopGuardKind.Msg)
	}
}

// TestRespondChatError_GuardNoticeCodes 验证守卫相关的对外标识稳定:
// notice code 与两条话术是前端契约(前端按 code 去重、按级别取文案),改名会让线上提示错位。
func TestRespondChatError_GuardNoticeCodes(t *testing.T) {
	if got := cCommon.ChatNoticeToolLoopGuard; got != "tool_loop_guard" {
		t.Errorf("ChatNoticeToolLoopGuard = %q, want tool_loop_guard", got)
	}
	if cCommon.ConstChatNoticeToolLoopGuardWarnMsg == "" || cCommon.ConstChatNoticeToolLoopGuardStopMsg == "" {
		t.Error("守卫命中提示的两条话术(Warn/Stop)都不应为空")
	}
	if cCommon.ConstChatNoticeToolLoopGuardWarnMsg == cCommon.ConstChatNoticeToolLoopGuardStopMsg {
		t.Error("Warn 与 Stop 级的用户话术应可区分")
	}
}
