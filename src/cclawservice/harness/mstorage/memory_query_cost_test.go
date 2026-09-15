/**
* 记忆维护的查询代价回归测试
*
* 背景(P1):轮次序号原本要查两次 MAX(t_turn.seq)—— 一次在 AppendTurn 内部分配序号,
* 一次在 TriggerMemoryMaintenance 里判断蒸馏节拍,而后者在**请求路径**上、
* 且在所有早退判断之前,等于每条用户消息白付一次 DB 往返。
* 现在 AppendTurn 直接把分配的序号返回给调用方,记忆维护复用它。
**/

package mstorage

import (
	"sync"
	"testing"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/model/dataaccessor"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/request"
)

// countingSessionAccessor 统计"会话内轮次序号"查询次数(其余方法直接委托真实实现)。
type countingSessionAccessor struct {
	dataaccessor.ISessionAccessor

	mu          sync.Mutex
	maxSeqCalls int
}

func (c *countingSessionAccessor) GetMaxTurnSeq(sessionId string) (int, error) {
	c.mu.Lock()
	c.maxSeqCalls++
	c.mu.Unlock()
	return c.ISessionAccessor.GetMaxTurnSeq(sessionId)
}

func (c *countingSessionAccessor) MaxSeqCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.maxSeqCalls
}

// TestAppendTurnReturnsSeqWithoutExtraQuery AppendTurn 必须返回本轮分配的轮次序号,
// 且只查一次 MAX(seq) —— 调用方(记忆维护)靠它判断蒸馏节拍,不再重复查询。
func TestAppendTurnReturnsSeqWithoutExtraQuery(t *testing.T) {
	db := newSessionTestDatabase(t)
	real, err := dataaccessor.GetSessionAccessor(db)
	if err != nil {
		t.Fatalf("构建会话访问器失败: %v", err)
	}

	spy := &countingSessionAccessor{ISessionAccessor: real}
	svc := &MemoryService{sessionAccessor: spy, enabled: true}

	agent := dataobject.TAgent{AgentId: "agent_001", AgentName: "测试助手", AgentDir: "default", OwnerId: "100001"}
	// 会话号必须与其它用例不同:历史文件按会话号落在磁盘上,写入器的 nextIndex 也是按会话号缓存,
	// 复用会话号会让两个用例互相污染序号。
	req := &request.ChatRequest{
		SessionId:   "7d4a9e13-5c26-4f8b-b0d1-3a9e6c52f7a4",
		TurnId:      "turn-a",
		ReqMsg:      "第一条",
		ChatChannel: 1,
	}
	session, ok := svc.Begin("tester", agent, req)
	if !ok {
		t.Fatal("建档失败")
	}
	cleanupHistoryDir(t, cCommon.NormalizeCompactId(req.SessionId))

	before := spy.MaxSeqCalls()
	if seq := svc.AppendTurn(session, req, "回复一", nil); seq != 1 {
		t.Fatalf("首轮序号应为 1,实际 %d", seq)
	}
	if got := spy.MaxSeqCalls() - before; got != 1 {
		t.Errorf("AppendTurn 应只查一次 MAX(seq),实际 %d 次", got)
	}

	second := &request.ChatRequest{SessionId: req.SessionId, TurnId: "turn-b", ReqMsg: "第二条", ChatChannel: 1}
	before = spy.MaxSeqCalls()
	if seq := svc.AppendTurn(session, second, "回复二", nil); seq != 2 {
		t.Fatalf("次轮序号应为 2,实际 %d", seq)
	}
	if got := spy.MaxSeqCalls() - before; got != 1 {
		t.Errorf("每轮应只查一次 MAX(seq),实际 %d 次", got)
	}

	// 文档化:TurnCount 本身就是一次 MAX(seq) 查询,因此**不应**与 AppendTurn 同时使用
	// (记忆维护只在 AppendTurn 没返回序号时(未落库)才退回调用它)。
	before = spy.MaxSeqCalls()
	if got := svc.TurnCount(req.SessionId); got != 2 {
		t.Errorf("TurnCount 应返回当前最大序号 2,实际 %d", got)
	}
	if got := spy.MaxSeqCalls() - before; got != 1 {
		t.Errorf("TurnCount 应恰好是一次 MAX(seq) 查询,实际 %d 次", got)
	}
}

// TestAppendTurnSeqZeroWhenNotRecorded 未真正落库时返回 0,调用方据此退回查询。
func TestAppendTurnSeqZeroWhenNotRecorded(t *testing.T) {
	db := newSessionTestDatabase(t)
	real, err := dataaccessor.GetSessionAccessor(db)
	if err != nil {
		t.Fatalf("构建会话访问器失败: %v", err)
	}

	// 未启用(旁路)= 不记录,序号必须是 0
	disabled := &MemoryService{sessionAccessor: real, enabled: false}
	if seq := disabled.AppendTurn(dataobject.TSession{SessionId: "s1"}, &request.ChatRequest{ReqMsg: "x"}, "r", nil); seq != 0 {
		t.Errorf("未启用时应返回 0,实际 %d", seq)
	}

	svc := &MemoryService{sessionAccessor: real, enabled: true}
	if seq := svc.AppendTurn(dataobject.TSession{SessionId: "s1"}, nil, "r", nil); seq != 0 {
		t.Errorf("请求为空时应返回 0,实际 %d", seq)
	}

	// 同一 turnId 重复上报(幂等跳过)不推进序号
	agent := dataobject.TAgent{AgentId: "agent_001", AgentName: "测试助手", AgentDir: "default2", OwnerId: "100001"}
	req := &request.ChatRequest{SessionId: "dup-seq", TurnId: "turn-dup", ReqMsg: "问题", ChatChannel: 1}
	session, ok := svc.Begin("tester", agent, req)
	if !ok {
		t.Fatal("建档失败")
	}
	cleanupHistoryDir(t, cCommon.NormalizeCompactId(req.SessionId))

	if seq := svc.AppendTurn(session, req, "回复", nil); seq != 1 {
		t.Fatalf("首轮序号应为 1,实际 %d", seq)
	}
	if seq := svc.AppendTurn(session, req, "回复", nil); seq != 0 {
		t.Errorf("重复上报应返回 0(未落库),实际 %d", seq)
	}
}
