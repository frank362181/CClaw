package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/harness/mstorage"
	"my.favorite/cclawservice/model/dataaccessor"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/request"
	"my.favorite/platformutility/config"
	"my.favorite/platformutility/database"
	"my.favorite/platformutility/database/core"
)

// 说明:本文件承载"必须与被测对象同包"的用例(ChatService 的非导出方法)。
// 从 mstorage 包迁出:service 依赖 mstorage,若在 mstorage 内引用 service 会构成 import cycle。

// TestNeedDistillNowCadence 蒸馏节拍:轮次达到门槛且正好落在间隔上时触发一次。
// turns 由 AppendTurn 返回(本轮序号 = 会话内最大序号),因此这里固定住"第 10/20/30… 轮触发"的语义。
func TestNeedDistillNowCadence(t *testing.T) {
	cases := map[int]bool{
		0: false, 1: false, 5: false, 6: false, 9: false,
		10: true, 11: false, 20: true, 30: true, 100: true,
	}

	for turns, want := range cases {
		if got := ctxmgr.NeedDistillNow(turns); got != want {
			t.Errorf("needDistillNow(%d) = %v, want %v", turns, got, want)
		}
	}
}

// newChatServiceTestDatabase 创建一个临时 SQLite 库并建好会话三表(测试辅助)。
func newChatServiceTestDatabase(t *testing.T) core.IBaseDatabase {
	t.Helper()

	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "cclaw_chat_service_test.db")

	// SQLite 库文件必须先存在:连接池用 common.ResolveFile 解析路径,该函数要求目标文件已存在
	f, err := os.Create(dbFile)
	if err != nil {
		t.Fatalf("创建测试库文件失败: %v", err)
	}
	_ = f.Close()

	iniPath := filepath.Join(tmpDir, "test.ini")
	iniContent := fmt.Sprintf(
		"[database]\ndbType=3\ndataDir=%q\ndbname=%q\nmax_connections=1\nmin_connections=1\n\n[logging]\npath=%q\n",
		tmpDir, filepath.Base(dbFile), filepath.Join(tmpDir, "log"),
	)
	if err := os.WriteFile(iniPath, []byte(iniContent), 0o644); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}

	helper, err := config.GetConfigurationHelper(iniPath)
	if err != nil {
		t.Fatalf("加载测试配置失败: %v", err)
	}
	if err = helper.LoadConfiguration(); err != nil {
		t.Fatalf("解析测试配置失败: %v", err)
	}

	db := database.GetGormDatabase(helper)
	if err = db.Open(); err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		helper.Destroy()
	})

	gormDb, ok := db.(*core.GormDatabase)
	if !ok {
		t.Fatalf("测试库应为 GormDatabase,实际 %T", db)
	}
	err = gormDb.GetDB().AutoMigrate(&dataobject.TSession{}, &dataobject.TTurn{}, &dataobject.TTurnTool{})
	if err != nil {
		t.Fatalf("建表失败: %v", err)
	}

	return db
}

// TestInterruptChatKeyScopedBySender 中断键必须与 ChatStream 注册键一致且按用户隔离。
// 回归点:曾出现"注册键用随机新 ID、中断用客户端 turnId"的错配,
// 导致中断 API 永远返回"未在聊天";同时不能丢掉发送者前缀,否则可跨用户冒用 turnId 中断。
func TestInterruptChatKeyScopedBySender(t *testing.T) {
	svc := &ChatService{} // InterruptChat 只依赖全局轮次流注册表,无需其他依赖
	registry := ctxmgr.GetTurnStreamMgr()

	const (
		owner  = "alice"
		other  = "bob"
		turnId = "3f2c1b0a9e8d4c7b8a615d4c3b2a1908"
	)
	key := cCommon.BuildTurnStreamKey(owner, turnId)

	cancelled := false
	registry.Register(key, func() { cancelled = true })
	t.Cleanup(func() { registry.Unregister(key) })

	// 他人拿着同一个 turnId 不能中断 alice 的流
	if err := svc.InterruptChat(other, turnId); err == nil {
		t.Error("其他用户不应能中断非自己的轮次")
	}
	if cancelled {
		t.Fatal("跨用户中断不应触发取消")
	}

	// 本人用同一个 turnId 可以中断
	if err := svc.InterruptChat(owner, turnId); err != nil {
		t.Fatalf("本人中断应成功: %v", err)
	}
	if !cancelled {
		t.Error("中断应触发取消函数")
	}

	// 中断后注册项应已清理,重复中断返回"未在聊天"
	if err := svc.InterruptChat(owner, turnId); err == nil {
		t.Error("重复中断应返回未在聊天错误")
	}
}

// TestNormalizeRequestIdConventions 业务标识前缀约定(跨单位/跨用户隔离的前提):
//   - sessionId 统一带 comCode 前缀(无论客户端是否提供),
//     否则 Web 端(总会自带 convId)落库的值不带前缀,与设计意图不符;
//   - turnId    不含 sender —— 只保证"单用户内唯一",跨用户隔离由 BuildTurnStreamKey 承担。
//
// 该约定一旦被破坏(例如给 turnId 也加上 sender 前缀),BuildTurnStreamKey 就退化成重复拼接,
// 更危险的是会让人误以为"turnId 已含 sender"从而删掉 key 上的前缀,重新引入跨用户中断漏洞。
func TestNormalizeRequestIdConventions(t *testing.T) {
	svc := &ChatService{}

	// 客户端未提供 sessionId / turnId → 走服务端兜底生成
	req := &request.ChatRequest{}
	svc.normalizeRequest("100001", "alice", req)

	if !strings.HasPrefix(req.SessionId, "100001") {
		t.Errorf("sessionId 应保留 comCode 前缀,实际 %q", req.SessionId)
	}
	// UUID 为 16 进制,不可能出现 "alice"(含 l/i),故此断言可判定"是否嵌入了 sender"
	if strings.Contains(req.TurnId, "alice") {
		t.Errorf("turnId 不应包含发送者,实际 %q", req.TurnId)
	}
	if len(req.TurnId) != 32 {
		t.Errorf("兜底生成的 turnId 应为 32 字符纯 UUID,实际 %d(%q)", len(req.TurnId), req.TurnId)
	}
	if len(req.SessionId) > cCommon.CompactIdLen || len(req.TurnId) > cCommon.CompactIdLen {
		t.Errorf("业务标识超出 CompactIdLen(%d): sessionId=%d turnId=%d",
			cCommon.CompactIdLen, len(req.SessionId), len(req.TurnId))
	}
	if req.ChatType != cCommon.ChatTypePrivate || req.ChatChannel != cCommon.ChatChannelWeb {
		t.Errorf("缺省 chatType/chatChannel 应回填默认值,实际 %d/%d", req.ChatType, req.ChatChannel)
	}

	// 提问时间:客户端未提供 → 以"请求到达时刻"兜底。
	// 若不兜底,AppendTurn 会用自己的当前时刻,chat_time 就变成完成时间。
	if req.GenAt <= 0 {
		t.Error("normalizeRequest 应以请求到达时刻兜底 req.GenAt")
	}

	// 客户端提供的时间必须保留(将来前端补发 genAt 时不能被覆盖)
	withGenAt := &request.ChatRequest{GenAt: 1770000000}
	svc.normalizeRequest("100001", "alice", withGenAt)
	if withGenAt.GenAt != 1770000000 {
		t.Errorf("客户端 genAt 应被保留,实际 %d", withGenAt.GenAt)
	}

	// 客户端提供的 turnId 必须原样规整保留,不能被服务端替换(否则客户端无法用该 id 中断)
	client := &request.ChatRequest{
		SessionId: "0b0ab7cf-4db5-4fe4-afd3-a548b55922c9",
		TurnId:    "3f2c1b0a-9e8d-4c7b-8a61-5d4c3b2a1908",
	}
	svc.normalizeRequest("100001", "alice", client)
	if client.TurnId != "3f2c1b0a9e8d4c7b8a615d4c3b2a1908" {
		t.Errorf("客户端 turnId 应被规整而非替换,实际 %q", client.TurnId)
	}
	// 客户端自带 sessionId 时也必须统一带上单位前缀(Web 端总是自带 convId)
	if want := "100001" + "0b0ab7cf4db54fe4afd3a548b55922c9"; client.SessionId != want {
		t.Errorf("客户端 sessionId 应统一带单位前缀:\n got %q\nwant %q", client.SessionId, want)
	}
	if len(client.SessionId) > cCommon.CompactIdLen {
		t.Errorf("带前缀的 sessionId 超出 CompactIdLen(%d): %d", cCommon.CompactIdLen, len(client.SessionId))
	}

	// 幂等:客户端回传服务端签发的 session_id(已带本单位前缀)时,不得重复叠加前缀。
	// 回归点:曾因无条件拼前缀,导致 comCode+(comCode+uuid) 被截断成另一个 session_id,
	// 续写/恢复会话被拆成新会话,历史接不上。
	replay := &request.ChatRequest{SessionId: client.SessionId}
	svc.normalizeRequest("100001", "alice", replay)
	if replay.SessionId != client.SessionId {
		t.Errorf("回传已带前缀的 sessionId 必须幂等:\n got %q\nwant %q", replay.SessionId, client.SessionId)
	}

	// 兜底生成的 sessionId 回传后同样必须幂等
	echo := &request.ChatRequest{SessionId: req.SessionId}
	svc.normalizeRequest("100001", "alice", echo)
	if echo.SessionId != req.SessionId {
		t.Errorf("回传服务端生成的 sessionId 必须幂等:\n got %q\nwant %q", echo.SessionId, req.SessionId)
	}
}

// TestChatTurnTimeSemantics 轮次时间语义:
//   - chat_time 取"请求到达时刻"(normalizeRequest 兜底写入 req.GenAt);
//   - resp_time 取"本轮结束时刻";
//   - 二者之差即响应耗时,因此必须可区分。
//
// 回归点:曾经两者都取 AppendTurn 时刻,差值恒为 0,
// 且会话历史文件里 user 记录的时间戳会晚于其后的 tool 记录。
func TestChatTurnTimeSemantics(t *testing.T) {
	db := newChatServiceTestDatabase(t)

	svc := mstorage.GetMemoryService(db, true)
	agent := dataobject.TAgent{AgentId: "agent_001", AgentDir: "default", OwnerId: "100001"}

	req := &request.ChatRequest{SessionId: "timing-1", TurnId: "turn-time-1", ReqMsg: "天气如何", ChatChannel: 1}
	(&ChatService{}).normalizeRequest("100001", "alice", req) // 等价于 prepareChat 中的规整
	if req.GenAt <= 0 {
		t.Fatal("normalizeRequest 应兜底 req.GenAt")
	}
	arrival := req.GenAt

	// 模拟一轮耗时:秒级粒度,故让结束时刻与到达时刻至少相差 1 秒
	time.Sleep(1100 * time.Millisecond)

	session, ok := svc.Begin("alice", agent, req)
	if !ok {
		t.Fatal("建档应成功")
	}
	t.Cleanup(func() {
		if dir, err := mstorage.GetMemoryFileWriter().MemoryDir(session.SessionId); err == nil {
			_ = os.RemoveAll(dir)
		}
	})
	svc.AppendTurn(session, req, "回复", nil)

	acc, err := dataaccessor.GetSessionAccessor(db)
	if err != nil {
		t.Fatalf("构建访问器失败: %v", err)
	}

	// 契约:落库 session_id 统一带单位前缀 —— 即使客户端自带 convId(Web 端总是如此)
	if !strings.HasPrefix(session.SessionId, "100001") {
		t.Errorf("落库 session_id 应带 comCode 前缀,实际 %q", session.SessionId)
	}
	stored, err := acc.GetSession(session.SessionId)
	if err != nil || stored == nil {
		t.Fatalf("按带前缀的 session_id 应能读回会话: err=%v", err)
	}
	turns, err := acc.GetTurns(session.SessionId)
	if err != nil {
		t.Fatalf("读取轮次失败: %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("应有 1 条轮次,实际 %d", len(turns))
	}

	turn := turns[0]
	if turn.ChatTime != arrival {
		t.Errorf("chat_time 应为请求到达时刻 %d,实际 %d(差 %d 秒)", arrival, turn.ChatTime, turn.ChatTime-arrival)
	}
	if turn.RespTime <= turn.ChatTime {
		t.Errorf("resp_time 应晚于 chat_time 以体现响应耗时,实际 chat=%d resp=%d", turn.ChatTime, turn.RespTime)
	}
}

// TestSessionAgentBindingIsolatedByComCode 会话→Agent 绑定的跨单位隔离。
//
// 绑定键就是 sessionId 本身(ctxmgr.bindingKey,不再叠加 comCode),这条用例锁定它成立的前提:
// sessionId 在**路由之前**已被 normalizeRequest 统一带上 comCode 前缀。
// 若将来有人在 normalizeRequest 里漏掉前缀、或把路由提到规整之前,
// 两个单位用同一个客户端 convId 就会命中同一条绑定 → 串 Agent、串会话历史。
func TestSessionAgentBindingIsolatedByComCode(t *testing.T) {
	svc := &ChatService{}
	registry := ctxmgr.GetChatSessionAgentMgr()

	// 两个单位拿到同一个客户端会话标识(两个单位的浏览器生成的 convId 完全可能相同)
	clientSessionId := "0b0ab7cf-4db5-4fe4-afd3-a548b55922c9"
	reqA := &request.ChatRequest{SessionId: clientSessionId}
	reqB := &request.ChatRequest{SessionId: clientSessionId}
	svc.normalizeRequest("100001", "alice", reqA)
	svc.normalizeRequest("100002", "bob", reqB)

	if reqA.SessionId == reqB.SessionId {
		t.Fatalf("不同单位的会话标识必须可区分(前缀即隔离依据): %q", reqA.SessionId)
	}

	registry.Set("100001", reqA.SessionId, "agent_001")
	registry.Set("100002", reqB.SessionId, "agent_002")
	t.Cleanup(func() {
		registry.ClearKey("100001", reqA.SessionId)
		registry.ClearKey("100002", reqB.SessionId)
	})

	gotA, okA := registry.Get("100001", reqA.SessionId)
	gotB, okB := registry.Get("100002", reqB.SessionId)
	if !okA || !okB || gotA != "agent_001" || gotB != "agent_002" {
		t.Fatalf("两个单位的绑定应各自独立: A=(%s,%v) B=(%s,%v)", gotA, okA, gotB, okB)
	}

	// 用 B 的 sessionId 去 A 的单位查询不应命中:前缀已保证键不同
	//
	// 注意这里断言的是"用 B 的 sessionId 查不到 A 的 agent",而不是"带错 comCode 就查不到":
	// 注册表的键**不含 comCode**(bindingKey 就是 sessionId),隔离完全由 sessionId 前缀承担,
	// 因此调用方必须只传规整后的本单位 sessionId —— 本用例锁定的正是这条前提。
	got, ok := registry.Get("100002", reqB.SessionId)
	if !ok || got != "agent_002" {
		t.Errorf("本单位绑定必须命中: (%s,%v)", got, ok)
	}

	// 客户端即使回传"他单位已带前缀的 sessionId",规整后也落不到对方的键上:
	// normalizeRequest 只会剥离**本单位**自己的前缀,所以会得到另一个键(不会串会话)。
	replay := &request.ChatRequest{SessionId: reqB.SessionId}
	svc.normalizeRequest("100001", "alice", replay)
	if replay.SessionId == reqB.SessionId {
		t.Errorf("回传他单位 sessionId 不应保持原值,实际 %q", replay.SessionId)
	}
	if _, ok := registry.Get("100001", replay.SessionId); ok {
		t.Error("回传他单位 sessionId 不应命中对方的绑定(键已不同)")
	}
}
