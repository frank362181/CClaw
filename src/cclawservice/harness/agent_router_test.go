/**
* AgentRouter 单元测试(方案C 混合路由)
*
* 通过 fake 仓库 + mock LLM 服务,验证路由优先级:
*   单 agent 短路 → 显式指定 → 会话绑定 → 关键词规则 → LLM 分类 → 默认兜底。
**/

package harness

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/model/repo"
	pCommon "my.favorite/platformutility/common"
)

// fakeAgentRepo 内存版 Agent 仓库:仅实现路由器依赖的方法。
type fakeAgentRepo struct {
	repo.IAgentRepository // 嵌入接口,未覆盖的方法调用时 panic(测试用)

	agents     []dataobject.TAgent
	providers  []dataobject.TLlm
	routeRules []dataobject.TAgentRouteRule
}

func (f *fakeAgentRepo) GetAgents(comCode string) []dataobject.TAgent {
	return f.agents
}

func (f *fakeAgentRepo) GetLLMProviders(comCode string) []dataobject.TLlm {
	return f.providers
}

func (f *fakeAgentRepo) GetAgentRouteRules(comCode string) []dataobject.TAgentRouteRule {
	return f.routeRules
}

// testAgents 构造两个测试 Agent:agent_001(专用,非默认)、agent_002(默认)。
func testAgents() []dataobject.TAgent {
	return []dataobject.TAgent{
		{AgentId: "agent_001", AgentName: "不良资产智能助手", Description: "不良资产标的评估、尽调、报告生成", PrimaryModel: "mock_model"},
		{AgentId: "agent_002", AgentName: "通用助手", Description: "通用助手", PrimaryModel: "mock_model", IsDefault: true},
	}
}

// newAgentRouterForTest 构造路由器并清空全局会话绑定,保证用例相互独立。
// 路由器为全局单例(GetAgentRouter),每次测试重新注入 fake 仓库即可。
func newAgentRouterForTest(rep repo.IAgentRepository) *AgentRouter {
	GetChatSessionAgentRegistry().Clear()
	return GetAgentRouter(rep)
}

// mockCompletionServer 返回一个返回固定 content 的 chat/completions mock 服务。
func mockCompletionServer(t *testing.T, content string) *httptest.Server {
	t.Helper()
	handler := func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []interface{}{
				map[string]interface{}{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": content,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(srv.Close)
	return srv
}

func TestAgentRouter_SingleAgentShortCircuit(t *testing.T) {
	rep := &fakeAgentRepo{agents: []dataobject.TAgent{{AgentId: "agent_001", AgentName: "only"}}}
	router := newAgentRouterForTest(rep)

	got, err := router.Resolve(context.Background(), "100001", "sess-1", "随便什么问题", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.AgentId != "agent_001" {
		t.Fatalf("agentId = %s, want agent_001", got.AgentId)
	}
}

func TestAgentRouter_ExplicitAgent(t *testing.T) {
	rep := &fakeAgentRepo{agents: testAgents()}
	router := newAgentRouterForTest(rep)

	// 显式命中
	got, err := router.Resolve(context.Background(), "100001", "sess-1", "你好", "agent_001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.AgentId != "agent_001" {
		t.Fatalf("agentId = %s, want agent_001", got.AgentId)
	}

	// 显式未命中 → 报错(与旧行为一致)
	if _, err := router.Resolve(context.Background(), "100001", "sess-2", "你好", "agent_not_exist"); err == nil {
		t.Fatal("expected error for unknown explicit agentId")
	}
}

func TestAgentRouter_SessionBinding(t *testing.T) {
	rep := &fakeAgentRepo{agents: testAgents()}
	router := newAgentRouterForTest(rep)

	// 首次:无关键词/无 provider → LLM 跳过 → 绑定默认 agent_002
	first, err := router.Resolve(context.Background(), "100001", "sess-1", "你好", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.AgentId != "agent_002" {
		t.Fatalf("first agentId = %s, want default agent_002", first.AgentId)
	}

	// 之后给该单位加上"命中 agent_001"的关键词规则
	rep.routeRules = []dataobject.TAgentRouteRule{
		{OwnerId: "100001", AgentId: "agent_001", Keywords: "不良资产", Priority: 10},
	}

	// 同一会话第二条消息即便命中关键词,仍沿用首次绑定的 agent_002(会话上下文一致)
	second, err := router.Resolve(context.Background(), "100001", "sess-1", "不良资产评估怎么做", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if second.AgentId != "agent_002" {
		t.Fatalf("bound agentId = %s, want agent_002 (session binding should win)", second.AgentId)
	}

	// 新会话:关键词路由生效 → agent_001
	third, err := router.Resolve(context.Background(), "100001", "sess-2", "不良资产评估怎么做", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if third.AgentId != "agent_001" {
		t.Fatalf("keyword routed agentId = %s, want agent_001", third.AgentId)
	}
}

func TestAgentRouter_KeywordRouting(t *testing.T) {
	rep := &fakeAgentRepo{
		agents: testAgents(),
		routeRules: []dataobject.TAgentRouteRule{
			{OwnerId: "100001", AgentId: "agent_001", Keywords: "不良资产,尽调,估值", Priority: 10},
		},
	}
	router := newAgentRouterForTest(rep)

	// 命中关键词 → agent_001
	got, err := router.Resolve(context.Background(), "100001", "sess-1", "帮我做不良资产的尽调", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.AgentId != "agent_001" {
		t.Fatalf("agentId = %s, want agent_001", got.AgentId)
	}

	// 未命中关键词 → 默认 agent_002
	got, err = router.Resolve(context.Background(), "100001", "sess-2", "今天天气怎么样", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.AgentId != "agent_002" {
		t.Fatalf("agentId = %s, want default agent_002", got.AgentId)
	}
}

func TestAgentRouter_LLMClassify(t *testing.T) {
	srv := mockCompletionServer(t, "agent_001")
	rep := &fakeAgentRepo{
		agents: testAgents(),
		providers: []dataobject.TLlm{
			{ProviderId: "mock_model", BaseUrl: srv.URL, Model: "m"},
		},
	}
	router := newAgentRouterForTest(rep)

	// LLM 返回合法 AgentId → 命中 agent_001(关键词为空,走到 LLM 层)
	got, err := router.Resolve(context.Background(), "100001", "sess-1", "请评估一下这个抵押物的价值", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.AgentId != "agent_001" {
		t.Fatalf("runtime classified agentId = %s, want agent_001", got.AgentId)
	}
}

func TestAgentRouter_LLMClassifyInvalidFallback(t *testing.T) {
	srv := mockCompletionServer(t, "hacker_agent") // 模型返回非配置的 agentId
	rep := &fakeAgentRepo{
		agents: testAgents(),
		providers: []dataobject.TLlm{
			{ProviderId: "mock_model", BaseUrl: srv.URL, Model: "m"},
		},
	}
	router := newAgentRouterForTest(rep)

	// 非法输出 → 校验失败,回落默认 agent_002
	got, err := router.Resolve(context.Background(), "100001", "sess-1", "请评估一下这个抵押物的价值", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.AgentId != "agent_002" {
		t.Fatalf("fallback agentId = %s, want default agent_002", got.AgentId)
	}
}

// TestAgentRouter_LLMClassify_FallbackProvider 验证:默认 agent 的模型在 t_llm 中无对应
// provider 时,resolveRoutingProvider 回退到任意可用 provider,LLM 意图判断仍能执行。
func TestAgentRouter_LLMClassify_FallbackProvider(t *testing.T) {
	srv := mockCompletionServer(t, "agent_001")
	rep := &fakeAgentRepo{
		agents: testAgents(), // 默认 agent(agent_002) primary_model=mock_model,但 providers 里没有它
		providers: []dataobject.TLlm{
			{ProviderId: "another_model", BaseUrl: srv.URL, Model: "m"},
		},
	}
	router := newAgentRouterForTest(rep)

	got, err := router.Resolve(context.Background(), "100001", "sess-1", "请评估一下这个抵押物的价值", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.AgentId != "agent_001" {
		t.Fatalf("runtime classified agentId = %s, want agent_001 (fallback provider used)", got.AgentId)
	}
}

// TestAgentRouter_LLMClassify_NoProvider 验证:comCode 下连任意可用 provider 都没有时,
// LLM 意图判断被跳过并回落默认 agent(不报错、不阻断聊天)。
func TestAgentRouter_LLMClassify_NoProvider(t *testing.T) {
	rep := &fakeAgentRepo{agents: testAgents()} // 无 providers
	router := newAgentRouterForTest(rep)

	got, err := router.Resolve(context.Background(), "100001", "sess-1", "请评估一下这个抵押物的价值", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.AgentId != "agent_002" {
		t.Fatalf("fallback agentId = %s, want default agent_002", got.AgentId)
	}
}

// TestAgentRouter_LLMClassify_UsesAgentsSummary 验证:LLM 意图分类的 system prompt 中,
// Agent 的"简介"取自其工作区 AGENTS.md 的 front matter summary,而不是数据库 description。
func TestAgentRouter_LLMClassify_UsesAgentsSummary(t *testing.T) {
	owner := "t-sum-owner"
	target := dataobject.TAgent{
		OwnerId:      owner,
		AgentId:      "agent_sum",
		AgentName:    "天气专用助手",
		Description:  "DB 旧简介(不应出现在 prompt)",
		PrimaryModel: "mock_model",
		AgentDir:     "agent-sum",
		IsDefault:    true,
	}
	other := dataobject.TAgent{
		OwnerId:      owner,
		AgentId:      "agent_general",
		AgentName:    "通用助手",
		Description:  "通用助手",
		PrimaryModel: "mock_model",
	}

	// 构造 target 的真实工作区:AGENTS.md 带 front matter summary(路径与 BuildAgentBootstrapRoot 一致)
	root := filepath.Join(pCommon.GetExecutableDir(), cCommon.AgentBootstrapDir, owner, target.AgentDir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir workspace error:%s", err.Error())
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"),
		[]byte("---\nsummary: \"路由摘要:专注实时天气与未来三天预报\"\n---\n# Rules\n- 天气\n"), 0o644); err != nil {
		t.Fatalf("write AGENTS.md error:%s", err.Error())
	}

	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = string(body)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []interface{}{map[string]interface{}{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": target.AgentId,
				},
			}},
		})
	}))
	t.Cleanup(srv.Close)

	rep := &fakeAgentRepo{
		agents: []dataobject.TAgent{target, other},
		providers: []dataobject.TLlm{
			{ProviderId: "mock_model", BaseUrl: srv.URL, Model: "m"},
		},
	}
	router := newAgentRouterForTest(rep)

	got, err := router.Resolve(context.Background(), owner, "sess-sum-1", "今天杭州天气如何", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.AgentId != target.AgentId {
		t.Fatalf("classified agentId = %s, want %s", got.AgentId, target.AgentId)
	}
	if !strings.Contains(captured, "路由摘要:专注实时天气与未来三天预报") {
		t.Fatalf("classify prompt should use AGENTS.md summary, got:\n%s", captured)
	}
	if strings.Contains(captured, "DB 旧简介") {
		t.Fatalf("classify prompt should NOT use DB description, got:\n%s", captured)
	}
}
