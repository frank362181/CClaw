package mstorage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cCommon "my.favorite/cclawservice/common"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/model/dataaccessor"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/request"
	"my.favorite/platformutility/config"
	"my.favorite/platformutility/database"
	"my.favorite/platformutility/database/core"
)

// 注意:涉及 ChatService(service 包)的用例已迁往 service 包。
// service 依赖 mstorage,在 mstorage 内引用 service 会构成 import cycle。

// newSessionTestDatabase 创建一个临时 SQLite 库并建好会话三表。
// 用 AutoMigrate 建表而非执行 data/ddl_sqlite3.sql:本用例要验证的是
// "数据对象 ↔ 列名 ↔ 访问器" 这条链路的映射是否正确
// (DDL 本身的语法与索引已由 sqlite3 CLI 单独校验)。
func newSessionTestDatabase(t *testing.T) core.IBaseDatabase {
	t.Helper()

	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "cclaw_session_test.db")

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

// TestChatSessionDoubleWrite 端到端验证阶段1双写:
// 会话建档(含跨消息复用)→ 轮次与工具落库 → 历史文件按序追加 → 索引与序号正确。
func TestChatSessionDoubleWrite(t *testing.T) {
	db := newSessionTestDatabase(t)

	svc := GetMemoryService(db, true)
	if !svc.Enabled() {
		t.Fatal("会话记录服务应处于启用状态")
	}

	sender := "tester"
	agent := dataobject.TAgent{AgentId: "agent_001", AgentName: "测试助手", AgentDir: "default", OwnerId: "100001"}

	// 前端 convId / turnId 均为 36 字符标准 UUID(crypto.randomUUID() 的产物)
	req := &request.ChatRequest{
		SessionId:   "0b0ab7cf-4db5-4fe4-afd3-a548b55922c9",
		TurnId:      "3f2c1b0a-9e8d-4c7b-8a61-5d4c3b2a1908",
		ReqMsg:      "帮我写一份不良资产尽调报告",
		ChatType:    0,
		ChatChannel: 1,
		GenAt:       1770000000,
	}
	wantSessionId := cCommon.NormalizeCompactId(req.SessionId)
	wantTurnId := cCommon.NormalizeCompactId(req.TurnId)

	session, ok := svc.Begin(sender, agent, req)
	if !ok {
		t.Fatal("首次 Begin 应建档成功")
	}
	cleanupHistoryDir(t, wantSessionId)

	// 会话标识必须已被规整到 VARCHAR(32) 以内,否则 PostgreSQL 会拒绝写入
	if session.SessionId != wantSessionId {
		t.Errorf("会话标识未规整: got %q, want %q", session.SessionId, wantSessionId)
	}
	if len(session.SessionId) > cCommon.CompactIdLen {
		t.Errorf("会话标识超出 DDL 长度: %d", len(session.SessionId))
	}
	if session.AgentId != agent.AgentId {
		t.Errorf("会话未绑定 Agent: got %q", session.AgentId)
	}
	if session.Sender != sender {
		t.Errorf("会话发送者错误: got %q", session.Sender)
	}
	if session.BeginTimestamp <= 0 || session.ChatType != req.ChatType || session.ChatChannel != req.ChatChannel {
		t.Errorf("会话元数据错误: %+v", session)
	}
	if !strings.HasPrefix(session.ChatData, hCommon.ChatMemoryFilePrefix) || !strings.HasSuffix(session.ChatData, hCommon.ChatMemoryFileExt) {
		t.Errorf("会话历史文件名不符合约定: %q", session.ChatData)
	}
	if session.SessionName == "" || len([]rune(session.SessionName)) > hCommon.SessionNameMaxLen {
		t.Errorf("会话名称生成异常: %q", session.SessionName)
	}

	// 同一会话的第二条消息必须复用同一条记录与同一个历史文件
	again, ok := svc.Begin(sender, agent, req)
	if !ok {
		t.Fatal("再次 Begin 应复用既有会话")
	}
	if again.ChatData != session.ChatData {
		t.Errorf("复用会话时历史文件名不应变化: got %q, want %q", again.ChatData, session.ChatData)
	}

	acc, err := dataaccessor.GetSessionAccessor(db)
	if err != nil {
		t.Fatalf("构建会话访问器失败: %v", err)
	}

	// ---------------- 第 1 轮:带 1 次工具调用 ----------------
	svc.AppendTurn(session, req, "已生成尽调报告", []TurnToolRecord{
		{ToolName: "get_weather", Result: `{"temp":26}`, CallTime: 1770000001, RespTime: 1770000002},
	})

	turns, err := acc.GetTurns(wantSessionId)
	if err != nil {
		t.Fatalf("读取轮次失败: %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("应有 1 条轮次记录,实际 %d", len(turns))
	}

	turn1 := turns[0]
	if turn1.Seq != 1 {
		t.Errorf("首轮序号应为 1,实际 %d", turn1.Seq)
	}
	// 客户端传入的 turnId 必须原样落库:它是中断与应答回显使用的同一标识
	if turn1.TurnId != wantTurnId {
		t.Errorf("turn_id 应与客户端传入的 turnId 一致: got %q, want %q", turn1.TurnId, wantTurnId)
	}
	if len(turn1.TurnId) > cCommon.CompactIdLen {
		t.Errorf("轮次业务主键不合规: %q", turn1.TurnId)
	}
	if turn1.ChatTime != req.GenAt {
		t.Errorf("提问时间应取请求 GenAt,实际 %d", turn1.ChatTime)
	}
	if turn1.RespTime <= 0 {
		t.Errorf("回复时间未写入: %d", turn1.RespTime)
	}
	// 历史文件中顺序为:问题(1) → 工具(2) → 回复(3)
	if turn1.ChatIndex != 1 || turn1.RespIndex != 3 {
		t.Errorf("首轮索引错误: chat_index=%d(期望 1), resp_index=%d(期望 3)", turn1.ChatIndex, turn1.RespIndex)
	}

	toolRows, err := acc.GetTurnTools(turn1.TurnId)
	if err != nil {
		t.Fatalf("读取轮内工具失败: %v", err)
	}
	if len(toolRows) != 1 {
		t.Fatalf("应有 1 条工具记录,实际 %d", len(toolRows))
	}
	if toolRows[0].ToolName != "get_weather" || toolRows[0].Seq != 1 || !toolRows[0].ToolCalled {
		t.Errorf("工具记录内容错误: %+v", toolRows[0])
	}
	if toolRows[0].ToolResp != `{"temp":26}` {
		t.Errorf("工具返回值摘要错误: %q", toolRows[0].ToolResp)
	}

	// ---------------- 第 2 轮:无工具调用(换一个 turnId) ----------------
	req2 := &request.ChatRequest{
		SessionId:   req.SessionId,
		TurnId:      "4a3b2c1d-0e9f-4a8b-9c70-6e5d4c3b2a19",
		ReqMsg:      "再补充一句",
		ChatType:    0,
		ChatChannel: 1,
		GenAt:       1770000100,
	}
	svc.AppendTurn(session, req2, "补充说明", nil)

	turns, err = acc.GetTurns(wantSessionId)
	if err != nil {
		t.Fatalf("读取轮次失败: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("应有 2 条轮次记录,实际 %d", len(turns))
	}
	if turns[1].Seq != 2 {
		t.Errorf("第二轮序号应为 2,实际 %d", turns[1].Seq)
	}
	// 第 2 轮无工具:问题(4) → 回复(5)
	if turns[1].ChatIndex != 4 || turns[1].RespIndex != 5 {
		t.Errorf("第二轮索引错误: chat_index=%d(期望 4), resp_index=%d(期望 5)", turns[1].ChatIndex, turns[1].RespIndex)
	}
	if turns[1].TurnId == turns[0].TurnId {
		t.Error("不同轮次的 turn_id 不应相同")
	}
	if turns[1].TurnId != cCommon.NormalizeCompactId(req2.TurnId) {
		t.Errorf("第二轮 turn_id 应与请求一致: got %q", turns[1].TurnId)
	}

	// ---------------- 幂等:同一 turnId 重复上报不得重复落库/重复写文件 ----------------
	contentBefore := readHistoryFile(t, wantSessionId, session.ChatData)
	svc.AppendTurn(session, req2, "补充说明", nil)   // 重复上报第 2 轮
	svc.AppendTurn(session, req, "已生成尽调报告", nil) // 重复上报第 1 轮

	turnsAfterRepeat, err := acc.GetTurns(wantSessionId)
	if err != nil {
		t.Fatalf("读取轮次失败: %v", err)
	}
	if len(turnsAfterRepeat) != 2 {
		t.Errorf("同一 turnId 重复上报不应新增轮次,期望 2 条,实际 %d", len(turnsAfterRepeat))
	}
	if got := readHistoryFile(t, wantSessionId, session.ChatData); got != contentBefore {
		t.Errorf("同一 turnId 重复上报不应重复追加历史文件")
	}

	// ---------------- 历史文件落盘内容 ----------------
	text := readHistoryFile(t, wantSessionId, session.ChatData)
	for _, want := range []string{
		"session_id: " + wantSessionId,
		"## [1] user",
		"帮我写一份不良资产尽调报告",
		"## [2] tool:get_weather",
		`{"temp":26}`,
		"## [3] assistant",
		"已生成尽调报告",
		"## [4] user",
		"## [5] assistant",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("历史文件缺少 %q,实际内容:\n%s", want, text)
		}
	}
}

// TestChatSessionServiceDisabled 关闭开关后必须完全旁路(不建会话、不落盘)。
func TestChatSessionServiceDisabled(t *testing.T) {
	db := newSessionTestDatabase(t)

	svc := GetMemoryService(db, false)
	if svc.Enabled() {
		t.Fatal("关闭开关后会话记录服务不应处于启用状态")
	}

	req := &request.ChatRequest{SessionId: "abc123", ReqMsg: "你好", ChatChannel: 1}
	if _, ok := svc.Begin("tester", dataobject.TAgent{AgentId: "agent_001"}, req); ok {
		t.Error("未启用时 Begin 应返回 ok=false")
	}

	// 未启用时调用其余方法必须安全空转(不 panic)
	svc.AppendTurn(dataobject.TSession{SessionId: "abc123"}, req, "回复", nil)
	if err := svc.EndSession("abc123", dataobject.SessionEndReasonManual); err != nil {
		t.Errorf("未启用时 EndSession 应静默返回 nil,实际 %v", err)
	}

	if _, err := GetMemoryFileWriter().MemoryDir("abc123"); err != nil {
		t.Fatalf("前置检查失败: %v", err)
	}
	if dir, _ := GetMemoryFileWriter().MemoryDir("abc123"); fileExists(dir) {
		t.Errorf("未启用时不应创建历史目录: %s", dir)
	}
}

// TestChatSessionRejectsForeignSender 同一 sessionId 被其他用户复用时必须拒绝记录。
// t_session 按 Excel 设计没有 owner 列,而 sessionId 由客户端提供,
// 因此 sender 是唯一可用的归属凭据 —— 不校验就会把 B 的问答追加进 A 的会话与历史文件。
func TestChatSessionRejectsForeignSender(t *testing.T) {
	db := newSessionTestDatabase(t)

	svc := GetMemoryService(db, true)
	agent := dataobject.TAgent{AgentId: "agent_001", AgentDir: "default", OwnerId: "100001"}
	req := &request.ChatRequest{SessionId: "share-1", TurnId: "turn-a", ReqMsg: "A 的问题", ChatChannel: 1}

	session, ok := svc.Begin("alice", agent, req)
	if !ok {
		t.Fatal("alice 首次建档应成功")
	}
	cleanupHistoryDir(t, session.SessionId)

	if _, ok = svc.Begin("bob", agent, req); ok {
		t.Error("其他用户复用同一 sessionId 应被拒绝记录")
	}
	if _, ok = svc.Begin("alice", agent, req); !ok {
		t.Error("会话归属者应能继续使用该会话")
	}
}

// cleanupHistoryDir 注册会话历史目录的清理(测试辅助)。
// 同时丢弃写入器的序号缓存:该缓存按文件路径键控,只删文件不清缓存会让
// 后续复用的同号会话从缓存序号继续(序号跳号、用例互相污染)。
func cleanupHistoryDir(t *testing.T, sessionId string) {
	t.Helper()

	t.Cleanup(func() {
		GetMemoryFileWriter().ForgetSession(sessionId)
		if dir, err := GetMemoryFileWriter().MemoryDir(sessionId); err == nil {
			_ = os.RemoveAll(dir)
		}
	})
}

// readHistoryFile 读取会话历史文件内容(测试辅助)。
func readHistoryFile(t *testing.T, sessionId, fileName string) string {
	t.Helper()

	dir, err := GetMemoryFileWriter().MemoryDir(sessionId)
	if err != nil {
		t.Fatalf("解析历史目录失败: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatalf("历史文件缺失: %v", err)
	}
	return string(content)
}

// fileExists 判断路径是否存在(测试辅助)。
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
