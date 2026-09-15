/**
* 会话收尾:结束标记 + 尾巴蒸馏的编排
*
* 收尾维度(NeedDistill 的 Closing)只是判定,这里锁住"谁把它打开、打开后会发生什么"。
**/

package service

import (
	"testing"

	"my.favorite/cclawservice/harness/mstorage"
	"my.favorite/cclawservice/model/dataaccessor"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/request"
)

// TestEndChatSessionMarksEndedAndClosesSession 结束会话必须落结束标记,并走一次收尾维护。
//
// 收尾维护内部的"要不要真调 LLM"由 mstorage.Distill 自己把关(记录不足/无信号都会跳过),
// 因此这里用只有 2 条记录的会话:既验证编排不炸(llmClient 在测试里是 nil),
// 又验证"没东西可蒸时不白烧调用"。
func TestEndChatSessionMarksEndedAndClosesSession(t *testing.T) {
	db := newChatServiceTestDatabase(t)

	mem := mstorage.GetMemoryService(db, true)
	if !mem.Enabled() {
		t.Fatal("记忆服务应可用")
	}

	agent := dataobject.TAgent{AgentId: "agent_001", AgentName: "测试助手", AgentDir: "default", OwnerId: "100001"}
	req := &request.ChatRequest{SessionId: "close-9f1c3a", TurnId: "turn-end", ReqMsg: "你好"}

	session, ok := mem.Begin("alice", agent, req)
	if !ok {
		t.Fatal("会话建档失败")
	}
	if session.ChatData == "" {
		t.Fatal("建档应生成历史文件名")
	}
	if seq := mem.AppendTurn(session, req, "你好,有什么可以帮你", nil); seq <= 0 {
		t.Fatalf("轮次落库失败: seq=%d", seq)
	}

	svc := &ChatService{memoryService: mem}
	if err := svc.EndChatSession(session, agent, dataobject.TLlm{}, dataobject.SessionEndReasonManual); err != nil {
		t.Fatalf("结束会话失败: %v", err)
	}

	accessor, err := dataaccessor.GetSessionAccessor(db)
	if err != nil {
		t.Fatalf("构建会话访问器失败: %v", err)
	}
	after, err := accessor.GetSession(session.SessionId)
	if err != nil || after == nil {
		t.Fatalf("回读会话失败: %v", err)
	}
	if !after.Ended {
		t.Error("结束标记应落库")
	}
	if after.EndReason != dataobject.SessionEndReasonManual {
		t.Errorf("结束原因应为手动(%d),实际 %d", dataobject.SessionEndReasonManual, after.EndReason)
	}
	if after.EndTimestamp <= 0 {
		t.Error("结束时间应落库")
	}
}

// TestEndChatSessionDisabledMemoryService 记忆服务不可用时不得报错(旁路语义)。
func TestEndChatSessionDisabledMemoryService(t *testing.T) {
	svc := &ChatService{}
	if err := svc.EndChatSession(dataobject.TSession{SessionId: "x"}, dataobject.TAgent{}, dataobject.TLlm{},
		dataobject.SessionEndReasonIdle); err != nil {
		t.Fatalf("记忆服务不可用时结束会话不应报错: %v", err)
	}
}
