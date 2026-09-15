/**
* 工具调用死循环守卫接进工具循环的端到端测试(httptest 假上游)
*
* 覆盖:
*   1. 命中后提示随 tool 消息回填给模型;
*   2. 模型收敛(给出最终答复)→ 本轮正常结束,不报错;
*   3. 模型无视提示继续重复 → **再次命中**后终止本轮(并带聊天域 Kind),不空转到 AgentToolLoopMax;
*   4. 模型听到提示换了工具 → 放行(不被误杀) —— 这条是"只判是否还调用工具"的回归;
*   5. 同步路径(TestRunToolLoop_*)行为与流式一致,且中止时带回已产出文本。
*
* 说明:用空 ToolSet 让工具执行必然失败("unknown tool"),结果稳定且无需真脚本;
* 提示语与规则判定由 ctxmgr 的单测覆盖,这里只验证"编排是否正确接上"。
**/

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"my.favorite/cclawservice/apperr"
	cCommon "my.favorite/cclawservice/common"
	myException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/harness"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/mstorage"
	"my.favorite/cclawservice/llmproto"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

// loopGuardHintMarker 守卫提示里稳定出现的片段(用于断言"提示已回填给模型")。
const loopGuardHintMarker = "检测到死循环"

// loopGuardWarnHintMarker 规则 2/3(Warn 级)提示里的稳定片段:"请换一种方法"/"请换一种方式"。
const loopGuardWarnHintMarker = "请换一种方"

// guardEchoScript 测试工具脚本:把 stdin 的参数字 JSON 回显到 stdout。
// 规则 3/5 的端到端必须让工具**执行成功**(否则会先命中"连续失败"),所以需要真实脚本;
// 回显参数又保证了"同工具不同参数 → 结果不同",不会误命中"完全相同"。
const guardEchoScript = "import json, sys\nraw = sys.stdin.read().strip()\nargs = json.loads(raw) if raw else {}\nprint(json.dumps(args, ensure_ascii=False))\n"

// guardMockResponse 假上游的一次应答:给了 toolCalls 就返回工具调用,否则返回文本。
type guardMockResponse struct {
	toolCalls []llmproto.ToolCall
	content   string
}

// guardTestProvider 构造指向假上游的 Provider(窗口足够大,避免 FitMessages 裁剪干扰断言)。
func guardTestProvider(baseURL string) dataobject.TLlm {
	return dataobject.TLlm{
		ProviderId:    "p_loop_guard",
		BaseUrl:       baseURL,
		ApiKey:        "test-key",
		Model:         "test-model",
		ContextWindow: 128000,
	}
}

// guardTestToolCall 构造一个工具调用(名与参数固定 → 守卫会判定为"完全相同的调用")。
func guardTestToolCall(id string) llmproto.ToolCall {
	return guardTestToolCallArgs(id, `{"path":"a.txt"}`)
}

// guardTestToolCallArgs 构造带指定参数的工具调用(参数不同 → 签名不同)。
func guardTestToolCallArgs(id, args string) llmproto.ToolCall {
	return llmproto.ToolCall{
		ID:       id,
		Type:     "function",
		Function: llmproto.ToolCallFunction{Name: "read_file", Arguments: args},
	}
}

// guardTestCallWithTool 构造指定工具名与参数的工具调用。
func guardTestCallWithTool(id, name, args string) llmproto.ToolCall {
	return llmproto.ToolCall{
		ID:       id,
		Type:     "function",
		Function: llmproto.ToolCallFunction{Name: name, Arguments: args},
	}
}

// guardTestSwitchToolCall 构造一个"换了策略"的工具调用(另一个工具 + 另一组参数)。
func guardTestSwitchToolCall(id string) llmproto.ToolCall {
	return llmproto.ToolCall{
		ID:       id,
		Type:     "function",
		Function: llmproto.ToolCallFunction{Name: "run_cmd", Arguments: `{"cmd":"ls"}`},
	}
}

// newGuardStreamServer 构造流式(SSE)假上游:第 n 次请求返回 responses[n-1],越界后复用最后一个;
// 请求体记录到 captured,供断言"守卫提示是否已回填给模型"。
func newGuardStreamServer(t *testing.T, responses []guardMockResponse, captured *[]llmproto.ChatCompletionRequest) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llmproto.ChatCompletionRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		*captured = append(*captured, req)

		idx := len(*captured) - 1
		if idx >= len(responses) {
			idx = len(responses) - 1
		}
		resp := responses[idx]

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if len(resp.toolCalls) == 0 {
			_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q},\"finish_reason\":\"stop\"}]}\n\n", resp.content)
		}
		// index 必须互不相同:流式累积器按 index 归并同轮多个工具调用
		for i, tc := range resp.toolCalls {
			_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":%d,\"id\":%q,\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":%q}}]},\"finish_reason\":\"tool_calls\"}]}\n\n",
				i, tc.ID, tc.Function.Name, tc.Function.Arguments)
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	return server
}

// guardHintDelivered 判断一次上游请求的 messages 里是否带着回填的守卫提示(规则 4/5 的提示语)。
func guardHintDelivered(req llmproto.ChatCompletionRequest) bool {
	return guardTextDelivered(req, loopGuardHintMarker)
}

// guardTextDelivered 判断某段文本是否已随 tool 消息回填给模型。
func guardTextDelivered(req llmproto.ChatCompletionRequest, text string) bool {
	for _, m := range req.Messages {
		if strings.Contains(m.Content, text) {
			return true
		}
	}

	return false
}

// newGuardToolLoopInput 构造工具循环的通用入参(空 ToolSet → 工具执行必然失败,结果稳定可预期)。
func newGuardToolLoopInput(baseURL string) (dataobject.TLlm, *harness.ToolSet, []llmproto.ChatMessage, *mstorage.TurnRecorder) {
	return guardTestProvider(baseURL), harness.LoadToolSet(pCommon.BlankString, nil),
		[]llmproto.ChatMessage{{Role: hCommon.RoleUser, Content: "读一下 a.txt"}}, mstorage.NewTurnRecorder()
}

// newGuardScriptToolSet 构造带真实脚本工具的 ToolSet(工具调用会成功,结果随参数变化):
// 临时目录里放一个 SKILL.md 声明若干工具,全部指向同一个回显脚本(解释器 python3,参数走 stdin)。
func newGuardScriptToolSet(t *testing.T, toolNames ...string) *harness.ToolSet {
	t.Helper()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "echo_args.py"), []byte(guardEchoScript), 0o755); err != nil {
		t.Fatalf("写入测试脚本失败: %v", err)
	}

	var tools strings.Builder
	for _, name := range toolNames {
		_, _ = fmt.Fprintf(&tools, "  - name: %s\n    description: echo args\n    script: echo_args.py\n", name)
	}
	skillDoc := fmt.Sprintf("---\nname: loop-guard-e2e\ndescription: loop guard e2e tools\nuser-invocable: false\ntools:\n%s---\n\n测试用技能:回显参数。\n", tools.String())

	skillPath := filepath.Join(root, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(skillDoc), 0o644); err != nil {
		t.Fatalf("写入测试 SKILL.md 失败: %v", err)
	}

	toolSet := harness.LoadToolSet(root, []string{skillPath})
	if got := len(toolSet.LLMTools()); got != len(toolNames) {
		t.Fatalf("测试工具集加载失败: tools=%d, want %d(检查 SKILL.md 声明格式)", got, len(toolNames))
	}

	return toolSet
}

// guardNoticeItem 一条下发前端的提示(notice 事件)。
type guardNoticeItem struct {
	code string
	msg  string
}

// guardNoticeRecorder 收集工具循环下发的 notice,用于断言"命中阶段的用户可见提示"。
type guardNoticeRecorder struct {
	items []guardNoticeItem
}

func (r *guardNoticeRecorder) onNotice(code, msg string) {
	r.items = append(r.items, guardNoticeItem{code: code, msg: msg})
}

// TestStreamToolLoop_GuardStopsRunawayLoop 验证流式路径:
// 第 3 次完全相同调用首次命中(回填提示) → 模型无视提示继续 → 第 4 次再次命中 → 终止本轮并返回 guard 专用错误,
// 不再空转到 AgentToolLoopMax。
func TestStreamToolLoop_GuardStopsRunawayLoop(t *testing.T) {
	var captured []llmproto.ChatCompletionRequest
	// 始终返回同一个工具调用:第 3 次首次命中(提示),第 4 次再次命中(终止),第 5 轮上游请求返回后即中止
	srv := newGuardStreamServer(t, []guardMockResponse{{toolCalls: []llmproto.ToolCall{guardTestToolCall("call_1")}}}, &captured)

	llmProvider, toolSet, messages, recorder := newGuardToolLoopInput(srv.URL)
	s := &ChatService{llmClient: harness.NewClient()}

	notices := &guardNoticeRecorder{}
	reply, _, err := s.streamToolLoop(context.Background(), llmProvider, messages, toolSet, nil, nil, nil, nil, notices.onNotice, recorder)
	if err == nil {
		t.Fatalf("模型无视守卫提示继续重复时应终止本轮, reply=%q", reply)
	}
	if kind := apperr.Of(err); kind != myException.ChatToolLoopGuardKind {
		t.Errorf("错误 Kind = %v, want %s (err=%v)", kind, myException.ChatToolLoopGuardKind.Name, err)
	}
	if len(captured) != 5 {
		t.Errorf("上游请求次数 = %d, want 5(第 3 次首次命中提示,第 4 次再次命中终止,不应跑到 %d)",
			len(captured), hCommon.AgentToolLoopMax)
	}
	if !guardHintDelivered(captured[3]) {
		t.Error("第 4 次上游请求的 messages 里应带着首次命中的守卫提示")
	}
	// 首次命中下发一条 notice(再次命中走错误通道),规则 4 是 Stop 级 → 用"已要求停止重试"话术
	if len(notices.items) != 1 {
		t.Fatalf("命中阶段应只下发 1 条提示, got %+v", notices.items)
	}
	if notices.items[0].code != cCommon.ChatNoticeToolLoopGuard {
		t.Errorf("notice code = %q, want %q", notices.items[0].code, cCommon.ChatNoticeToolLoopGuard)
	}
	if notices.items[0].msg != cCommon.ConstChatNoticeToolLoopGuardStopMsg {
		t.Errorf("notice msg = %q, want %q(规则 4 为 Stop 级)", notices.items[0].msg, cCommon.ConstChatNoticeToolLoopGuardStopMsg)
	}
}

// TestStreamToolLoop_GuardAllowsStrategySwitch 验证"听到提示换了方法"不会被误杀:
// 这是"提示后只要还调用工具就硬停"的回归 —— 那种写法会在第 4 轮把已自纠的轮次掐死。
func TestStreamToolLoop_GuardAllowsStrategySwitch(t *testing.T) {
	const finalAnswer = "已经改用其它方式读取,内容如下……"

	var captured []llmproto.ChatCompletionRequest
	toolCall := guardTestToolCall("call_1")
	srv := newGuardStreamServer(t, []guardMockResponse{
		{toolCalls: []llmproto.ToolCall{toolCall}},                               // 第 1 轮
		{toolCalls: []llmproto.ToolCall{toolCall}},                               // 第 2 轮
		{toolCalls: []llmproto.ToolCall{toolCall}},                               // 第 3 轮:首次命中,提示回填
		{toolCalls: []llmproto.ToolCall{guardTestSwitchToolCall("call_switch")}}, // 第 4 轮:换工具(自纠)
		{content: finalAnswer},                                                   // 第 5 轮:给出最终答复
	}, &captured)

	llmProvider, toolSet, messages, recorder := newGuardToolLoopInput(srv.URL)
	s := &ChatService{llmClient: harness.NewClient()}

	reply, _, err := s.streamToolLoop(context.Background(), llmProvider, messages, toolSet, nil, nil, nil, nil, nil, recorder)
	if err != nil {
		t.Fatalf("模型已换策略,本轮不应报错: %v", err)
	}
	if reply != finalAnswer {
		t.Errorf("reply = %q, want %q", reply, finalAnswer)
	}
	if len(captured) != 5 {
		t.Errorf("上游请求次数 = %d, want 5(提示后换工具应被放行,继续跑到模型给出答复)", len(captured))
	}
}

// TestStreamToolLoop_GuardHintFedBackAndModelConverges 验证提示回填后模型给出最终答复 → 本轮正常结束(不报错)。
func TestStreamToolLoop_GuardHintFedBackAndModelConverges(t *testing.T) {
	const finalAnswer = "我陷入了重复读取,已停止并说明情况。"

	var captured []llmproto.ChatCompletionRequest
	toolCall := guardTestToolCall("call_1")
	srv := newGuardStreamServer(t, []guardMockResponse{
		{toolCalls: []llmproto.ToolCall{toolCall}}, // 第 1 轮
		{toolCalls: []llmproto.ToolCall{toolCall}}, // 第 2 轮
		{toolCalls: []llmproto.ToolCall{toolCall}}, // 第 3 轮:首次命中,提示回填
		{content: finalAnswer},                     // 第 4 轮:模型收敛(不再请求工具)
	}, &captured)

	llmProvider, toolSet, messages, recorder := newGuardToolLoopInput(srv.URL)
	s := &ChatService{llmClient: harness.NewClient()}

	reply, _, err := s.streamToolLoop(context.Background(), llmProvider, messages, toolSet, nil, nil, nil, nil, nil, recorder)
	if err != nil {
		t.Fatalf("模型已收敛,本轮不应报错: %v", err)
	}
	if reply != finalAnswer {
		t.Errorf("reply = %q, want %q", reply, finalAnswer)
	}
	if len(captured) != 4 {
		t.Fatalf("上游请求次数 = %d, want 4", len(captured))
	}
	if !guardHintDelivered(captured[3]) {
		t.Error("第 4 轮上游请求的 messages 里应带着守卫提示")
	}
}

// TestStreamToolLoop_GuardCountsParallelCallsInOrder 验证同轮多个工具(并发执行)按"模型声明的顺序"计数:
// 第 1 轮声明 2 个相同调用 + 第 2 轮 1 个 → 第 3 次首次命中 → 第 3 轮第 4 次再次命中 → 第 4 轮终止。
func TestStreamToolLoop_GuardCountsParallelCallsInOrder(t *testing.T) {
	var captured []llmproto.ChatCompletionRequest
	srv := newGuardStreamServer(t, []guardMockResponse{
		{toolCalls: []llmproto.ToolCall{guardTestToolCall("call_a"), guardTestToolCall("call_b")}}, // 同轮 2 个
		{toolCalls: []llmproto.ToolCall{guardTestToolCall("call_c")}},                              // 第 3 次 → 首次命中
	}, &captured)

	llmProvider, toolSet, messages, recorder := newGuardToolLoopInput(srv.URL)
	s := &ChatService{llmClient: harness.NewClient()}

	if _, _, err := s.streamToolLoop(context.Background(), llmProvider, messages, toolSet, nil, nil, nil, nil, nil, recorder); err == nil {
		t.Fatal("模型无视守卫提示继续重复时应终止本轮")
	}
	if len(captured) != 4 {
		t.Errorf("上游请求次数 = %d, want 4(同轮 2 个调用已计入,第 3 次首次命中,第 4 次再次命中后终止)", len(captured))
	}
	if !guardHintDelivered(captured[3]) {
		t.Error("第 4 次上游请求的 messages 里应带着守卫提示")
	}
}

// TestRunToolLoop_GuardStopsRunawayLoop 验证同步路径同样接入守卫(行为与流式一致),
// 且中止时把本轮已产出的文本带回调用方(不再"内容全丢")。
func TestRunToolLoop_GuardStopsRunawayLoop(t *testing.T) {
	const partialReply = "部分输出"

	var captured []llmproto.ChatCompletionRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llmproto.ChatCompletionRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		captured = append(captured, req)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"id":"c1","model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":%q,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a.txt\"}"}}]},"finish_reason":"tool_calls"}]}`, partialReply)
	}))
	t.Cleanup(srv.Close)

	llmProvider, toolSet, messages, recorder := newGuardToolLoopInput(srv.URL)
	s := &ChatService{llmClient: harness.NewClient()}

	reply, err := s.runToolLoop(context.Background(), llmProvider, messages, toolSet, recorder)
	if err == nil {
		t.Fatal("模型无视守卫提示继续重复时应终止本轮")
	}
	if kind := apperr.Of(err); kind != myException.ChatToolLoopGuardKind {
		t.Errorf("错误 Kind = %v, want %s (err=%v)", kind, myException.ChatToolLoopGuardKind.Name, err)
	}
	if reply != partialReply {
		t.Errorf("reply = %q, want %q(终止时也要把已产出文本带回调用方)", reply, partialReply)
	}
	if len(captured) != 5 {
		t.Errorf("上游请求次数 = %d, want 5(与流式路径一致:首次命中提示、再次命中终止)", len(captured))
	}
	if !guardHintDelivered(captured[3]) {
		t.Error("第 4 次上游请求的 messages 里应带着守卫提示")
	}
}

// TestStreamToolLoop_GuardNoticeLevelWarn 验证用户提示按判定级别取话术:
// 连续失败(规则 2)属 Warn 级 → 提示是"已提醒它换一种方式",而不是 Stop 级的"已要求停止重试"。
func TestStreamToolLoop_GuardNoticeLevelWarn(t *testing.T) {
	const (
		finalAnswer = "换个思路后已完成。"
		failRounds  = 5 //与 ctxmgr 的"连续失败"阈值一致:第 5 次失败时首次命中
	)

	var captured []llmproto.ChatCompletionRequest
	// 每轮都失败且参数各不相同:只可能命中"连续失败"规则
	responses := make([]guardMockResponse, 0, failRounds+1)
	for i := 1; i <= failRounds; i++ {
		call := llmproto.ToolCall{
			ID:       fmt.Sprintf("call_%d", i),
			Type:     "function",
			Function: llmproto.ToolCallFunction{Name: "read_file", Arguments: fmt.Sprintf(`{"path":"p%d.txt"}`, i)},
		}
		responses = append(responses, guardMockResponse{toolCalls: []llmproto.ToolCall{call}})
	}
	responses = append(responses, guardMockResponse{content: finalAnswer}) // 命中提示后模型给出答复

	srv := newGuardStreamServer(t, responses, &captured)

	llmProvider, toolSet, messages, recorder := newGuardToolLoopInput(srv.URL)
	s := &ChatService{llmClient: harness.NewClient()}

	notices := &guardNoticeRecorder{}
	reply, _, err := s.streamToolLoop(context.Background(), llmProvider, messages, toolSet, nil, nil, nil, nil, notices.onNotice, recorder)
	if err != nil {
		t.Fatalf("模型命中提示后给出答复,本轮不应报错: %v", err)
	}
	if reply != finalAnswer {
		t.Errorf("reply = %q, want %q", reply, finalAnswer)
	}
	if len(notices.items) != 1 {
		t.Fatalf("首次命中应下发 1 条提示, got %+v", notices.items)
	}
	if notices.items[0].code != cCommon.ChatNoticeToolLoopGuard {
		t.Errorf("notice code = %q, want %q", notices.items[0].code, cCommon.ChatNoticeToolLoopGuard)
	}
	if notices.items[0].msg != cCommon.ConstChatNoticeToolLoopGuardWarnMsg {
		t.Errorf("notice msg = %q, want %q(规则 2 为 Warn 级)", notices.items[0].msg, cCommon.ConstChatNoticeToolLoopGuardWarnMsg)
	}
}

// TestStreamToolLoop_GuardRule2StopsAfterWarning 验证规则 2(连续失败,Warn 级)的完整链路:
// 第 5 次失败首次命中 → 下发"已提醒它换一种方式" + 提示回填 → 模型继续失败 → 再次命中 → 终止本轮。
func TestStreamToolLoop_GuardRule2StopsAfterWarning(t *testing.T) {
	const failRounds = 5 //与 ctxmgr 的"连续失败"阈值一致:第 5 次失败首次命中

	var captured []llmproto.ChatCompletionRequest
	responses := make([]guardMockResponse, 0, failRounds+2)
	for i := 1; i <= failRounds+2; i++ {
		// 参数每轮都不同:只可能命中"连续失败"(参数若相同会在第 3 次先命中"完全相同")
		responses = append(responses, guardMockResponse{
			toolCalls: []llmproto.ToolCall{guardTestToolCallArgs(fmt.Sprintf("call_%d", i), fmt.Sprintf(`{"path":"p%d.txt"}`, i))},
		})
	}
	srv := newGuardStreamServer(t, responses, &captured)

	// 空 ToolSet:工具执行必然失败,正好构造"连续失败"
	llmProvider, toolSet, messages, recorder := newGuardToolLoopInput(srv.URL)
	s := &ChatService{llmClient: harness.NewClient()}

	notices := &guardNoticeRecorder{}
	if _, _, err := s.streamToolLoop(context.Background(), llmProvider, messages, toolSet, nil, nil, nil, nil, notices.onNotice, recorder); err == nil {
		t.Fatal("模型无视守卫提示继续失败时应终止本轮")
	} else if kind := apperr.Of(err); kind != myException.ChatToolLoopGuardKind {
		t.Errorf("错误 Kind = %v, want %s (err=%v)", kind, myException.ChatToolLoopGuardKind.Name, err)
	}
	// R1..R4 无命中;R5 首次命中(提示);R6 再次命中;R7 终止
	if len(captured) != failRounds+2 {
		t.Errorf("上游请求次数 = %d, want %d", len(captured), failRounds+2)
	}
	if !guardTextDelivered(captured[len(captured)-1], loopGuardWarnHintMarker) {
		t.Error("最后一次上游请求的 messages 里应带着规则 2 的提示")
	}
	if len(notices.items) != 1 || notices.items[0].msg != cCommon.ConstChatNoticeToolLoopGuardWarnMsg {
		t.Errorf("首次命中应只下发 1 条 Warn 提示, got %+v", notices.items)
	}
}

// TestStreamToolLoop_GuardRule3SameToolStopsRunaway 验证规则 3(同一工具连续调用 10 次、参数各不相同,Warn 级):
// 第 10 次首次命中 → 第 11 次再次命中 → 第 12 轮终止本轮。
// 注:这里必须让工具**执行成功**(真脚本工具),否则会先命中"连续失败"(规则 2)。
func TestStreamToolLoop_GuardRule3SameToolStopsRunaway(t *testing.T) {
	const sameToolRounds = 10 //与 ctxmgr 的"同一工具连续调用"阈值一致

	var captured []llmproto.ChatCompletionRequest
	responses := make([]guardMockResponse, 0, sameToolRounds+2)
	for i := 1; i <= sameToolRounds+2; i++ {
		responses = append(responses, guardMockResponse{
			toolCalls: []llmproto.ToolCall{guardTestToolCallArgs(fmt.Sprintf("call_%d", i), fmt.Sprintf(`{"path":"p%d.txt"}`, i))},
		})
	}
	srv := newGuardStreamServer(t, responses, &captured)

	llmProvider, _, messages, recorder := newGuardToolLoopInput(srv.URL)
	s := &ChatService{llmClient: harness.NewClient()}
	toolSet := newGuardScriptToolSet(t, "read_file")

	notices := &guardNoticeRecorder{}
	if _, _, err := s.streamToolLoop(context.Background(), llmProvider, messages, toolSet, nil, nil, nil, nil, notices.onNotice, recorder); err == nil {
		t.Fatal("同一工具连续调用远超阈值仍未收敛时应终止本轮")
	} else if kind := apperr.Of(err); kind != myException.ChatToolLoopGuardKind {
		t.Errorf("错误 Kind = %v, want %s (err=%v)", kind, myException.ChatToolLoopGuardKind.Name, err)
	}
	if len(captured) != sameToolRounds+2 {
		t.Errorf("上游请求次数 = %d, want %d(第 10 次首次命中,第 11 次再次命中,第 12 轮终止)", len(captured), sameToolRounds+2)
	}
	if !guardTextDelivered(captured[len(captured)-1], loopGuardWarnHintMarker) {
		t.Error("最后一次上游请求的 messages 里应带着规则 3 的提示")
	}
	if len(notices.items) != 1 || notices.items[0].msg != cCommon.ConstChatNoticeToolLoopGuardWarnMsg {
		t.Errorf("首次命中应只下发 1 条 Warn 提示, got %+v", notices.items)
	}
}

// TestStreamToolLoop_GuardRule5AlternatingStopsRunaway 验证规则 5(二元交替 A,B,A,B…,Stop 级):
// 第 6 次(交替 3 轮)首次命中 → 下发"已要求模型停止重试" → 第 7 次再次命中 → 第 8 轮终止本轮。
func TestStreamToolLoop_GuardRule5AlternatingStopsRunaway(t *testing.T) {
	const alternatingRounds = 3 //与 ctxmgr 的"二元交替"阈值一致:A,B 交替 3 轮 = 6 次调用

	var captured []llmproto.ChatCompletionRequest
	responses := make([]guardMockResponse, 0, alternatingRounds*2+2)
	for i := 1; i <= alternatingRounds*2+2; i++ {
		if i%2 == 1 {
			responses = append(responses, guardMockResponse{
				toolCalls: []llmproto.ToolCall{guardTestCallWithTool(fmt.Sprintf("call_%d", i), "read_file", `{"path":"a.txt"}`)},
			})
			continue
		}
		responses = append(responses, guardMockResponse{
			toolCalls: []llmproto.ToolCall{guardTestCallWithTool(fmt.Sprintf("call_%d", i), "run_cmd", `{"cmd":"ls"}`)},
		})
	}
	srv := newGuardStreamServer(t, responses, &captured)

	llmProvider, _, messages, recorder := newGuardToolLoopInput(srv.URL)
	s := &ChatService{llmClient: harness.NewClient()}
	toolSet := newGuardScriptToolSet(t, "read_file", "run_cmd")

	notices := &guardNoticeRecorder{}
	if _, _, err := s.streamToolLoop(context.Background(), llmProvider, messages, toolSet, nil, nil, nil, nil, notices.onNotice, recorder); err == nil {
		t.Fatal("A/B 交替重复超过阈值仍未收敛时应终止本轮")
	} else if kind := apperr.Of(err); kind != myException.ChatToolLoopGuardKind {
		t.Errorf("错误 Kind = %v, want %s (err=%v)", kind, myException.ChatToolLoopGuardKind.Name, err)
	}
	if len(captured) != alternatingRounds*2+2 {
		t.Errorf("上游请求次数 = %d, want %d(第 6 次首次命中,第 7 次再次命中,第 8 轮终止)", len(captured), alternatingRounds*2+2)
	}
	if !guardHintDelivered(captured[len(captured)-1]) {
		t.Error("最后一次上游请求的 messages 里应带着规则 5 的提示")
	}
	if len(notices.items) != 1 || notices.items[0].msg != cCommon.ConstChatNoticeToolLoopGuardStopMsg {
		t.Errorf("首次命中应只下发 1 条 Stop 提示, got %+v", notices.items)
	}
}
