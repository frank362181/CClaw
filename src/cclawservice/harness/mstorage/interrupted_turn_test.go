package mstorage

import (
	"strings"
	"testing"

	"my.favorite/cclawservice/model/dataaccessor"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/request"
)

// TestAppendInterruptedTurnMarksRecord 中断轮次必须留痕:
//   - 历史文件里助手记录带 interrupted 标记,正文为中断前已产出的部分;
//   - t_turn.resp_index 指向该记录(客户端已看到内容,故不能丢)。
func TestAppendInterruptedTurnMarksRecord(t *testing.T) {
	db := newSessionTestDatabase(t)
	svc := GetMemoryService(db, true)
	agent := dataobject.TAgent{AgentId: "agent_001", AgentDir: "default", OwnerId: "100001"}

	req := &request.ChatRequest{SessionId: "interrupted-1", TurnId: "turnint1", ReqMsg: "帮我写一份报告", GenAt: 1770000000, ChatChannel: 1}
	session, ok := svc.Begin("alice", agent, req)
	if !ok {
		t.Fatal("建档应成功")
	}
	cleanupHistoryDir(t, session.SessionId)

	svc.AppendInterruptedTurn(session, req, "报告的开头部分……", nil, "interrupted by user")

	acc, err := dataaccessor.GetSessionAccessor(db)
	if err != nil {
		t.Fatalf("构建访问器失败: %v", err)
	}
	turn, err := acc.GetTurn("turnint1")
	if err != nil || turn == nil {
		t.Fatalf("中断轮次应落库: err=%v", err)
	}
	if turn.RespIndex <= 0 {
		t.Errorf("已产出内容时 resp_index 应指向助手记录,实际 %d", turn.RespIndex)
	}

	records, err := svc.ReadRecords(session)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("应写入 user + assistant 两条记录: %+v", records)
	}
	assistant := records[1]
	if !assistant.IsInterrupted() {
		t.Errorf("助手记录应带中断标记: %+v", assistant)
	}
	if assistant.Content != "报告的开头部分……" {
		t.Errorf("部分回复内容应保留: %q", assistant.Content)
	}
	if assistant.Index != turn.RespIndex {
		t.Errorf("resp_index(%d) 应等于助手记录序号(%d)", turn.RespIndex, assistant.Index)
	}
}

// TestAppendInterruptedTurnWithoutContent 中断时还没有任何产出:不写助手记录(resp_index = 0),
// 避免在历史里留下空记录。
func TestAppendInterruptedTurnWithoutContent(t *testing.T) {
	db := newSessionTestDatabase(t)
	svc := GetMemoryService(db, true)
	agent := dataobject.TAgent{AgentId: "agent_001", AgentDir: "default", OwnerId: "100001"}

	req := &request.ChatRequest{SessionId: "interrupted-2", TurnId: "turnint2", ReqMsg: "随便问问", GenAt: 1770000100, ChatChannel: 1}
	session, ok := svc.Begin("alice", agent, req)
	if !ok {
		t.Fatal("建档应成功")
	}
	cleanupHistoryDir(t, session.SessionId)

	svc.AppendInterruptedTurn(session, req, "", nil, "run timeout")

	acc, _ := dataaccessor.GetSessionAccessor(db)
	turn, err := acc.GetTurn("turnint2")
	if err != nil || turn == nil {
		t.Fatalf("中断轮次应落库: err=%v", err)
	}
	if turn.RespIndex != 0 {
		t.Errorf("无产出时 resp_index 应为 0,实际 %d", turn.RespIndex)
	}

	records, _ := svc.ReadRecords(session)
	for _, rec := range records {
		if rec.RecordType == HistoryRecordAssistant {
			t.Fatalf("无产出时不应写助手记录: %+v", rec)
		}
	}
	if len(records) != 1 || records[0].RecordType != HistoryRecordUser {
		t.Errorf("应只保留用户提问: %+v", records)
	}
}

// TestAppendInterruptedTurnRecordsTools 中断前已执行过的工具同样要落库(副作用需要可审计)。
func TestAppendInterruptedTurnRecordsTools(t *testing.T) {
	db := newSessionTestDatabase(t)
	svc := GetMemoryService(db, true)
	agent := dataobject.TAgent{AgentId: "agent_001", AgentDir: "default", OwnerId: "100001"}

	req := &request.ChatRequest{SessionId: "interrupted-3", TurnId: "turnint3", ReqMsg: "查一下天气", GenAt: 1770000200, ChatChannel: 1}
	session, ok := svc.Begin("alice", agent, req)
	if !ok {
		t.Fatal("建档应成功")
	}
	cleanupHistoryDir(t, session.SessionId)

	tools := []TurnToolRecord{
		{ToolName: "get_weather", Result: `{"city":"杭州","weather":"晴"}`, CallTime: 1770000201, RespTime: 1770000202},
	}
	svc.AppendInterruptedTurn(session, req, "杭州明天…", tools, "interrupted by user")

	acc, _ := dataaccessor.GetSessionAccessor(db)
	rows, err := acc.GetTurnTools("turnint3")
	if err != nil {
		t.Fatalf("读取轮内工具失败: %v", err)
	}
	if len(rows) != 1 || rows[0].ToolName != "get_weather" {
		t.Fatalf("中断前的工具调用应落库: %+v", rows)
	}

	records, _ := svc.ReadRecords(session)
	var sawTool, sawInterrupted bool
	for _, rec := range records {
		switch {
		case rec.RecordType == HistoryRecordTool:
			sawTool = true
		case rec.IsInterrupted():
			sawInterrupted = true
		}
	}
	if !sawTool || !sawInterrupted {
		t.Errorf("历史文件应同时含工具记录与中断标记: tool=%v interrupted=%v", sawTool, sawInterrupted)
	}

	// 记录顺序必须是 问题 → 工具 → 回复(与正常轮次一致)
	if len(records) != 3 || records[0].RecordType != HistoryRecordUser ||
		records[1].RecordType != HistoryRecordTool || !records[2].IsInterrupted() {
		t.Fatalf("记录顺序不正确: %+v", records)
	}
}

// TestAppendInterruptedTurnIdempotent 同一 turnId 重复上报(客户端重试)不得重复写入。
func TestAppendInterruptedTurnIdempotent(t *testing.T) {
	db := newSessionTestDatabase(t)
	svc := GetMemoryService(db, true)
	agent := dataobject.TAgent{AgentId: "agent_001", AgentDir: "default", OwnerId: "100001"}

	req := &request.ChatRequest{SessionId: "interrupted-4", TurnId: "turn-int-4", ReqMsg: "重复提交", GenAt: 1770000300, ChatChannel: 1}
	session, ok := svc.Begin("alice", agent, req)
	if !ok {
		t.Fatal("建档应成功")
	}
	cleanupHistoryDir(t, session.SessionId)

	svc.AppendInterruptedTurn(session, req, "第一次的部分回复", nil, "interrupted by user")
	svc.AppendInterruptedTurn(session, req, "第二次的部分回复", nil, "interrupted by user")

	records, _ := svc.ReadRecords(session)
	if len(records) != 2 {
		t.Fatalf("重复上报不应重复写入: %+v", records)
	}
	if !strings.Contains(records[1].Content, "第一次") {
		t.Errorf("首次记录应保持不被覆盖: %q", records[1].Content)
	}
}
