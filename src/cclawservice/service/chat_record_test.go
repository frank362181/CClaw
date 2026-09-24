package service

import (
	"fmt"
	"testing"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/harness/mstorage"
	"my.favorite/cclawservice/request"
)

// TestNoteRecordNotAvailable 锁住"会话归属冲突不静默丢弃"这条契约。
//
// 背景:会话已属于另一个 sender 时,本轮不写历史、不写 t_turn —— 这是正确的(不能把
// 别人的问答写进他人会话),但**不能静默**:否则表现是"每轮都答得出、历史永远为空",
// 用户与运维都无从定位(见 history-memory-review.md A2 / interrupt-memory-chain.md M4)。
func TestNoteRecordNotAvailable(t *testing.T) {
	svc := &ChatService{}
	chatReq := &request.ChatRequest{SessionId: "s1"}

	var code, msg string
	capture := func(c, m string) { code, msg = c, m }

	// 1. 其它原因(未启用/建档失败等):不打扰用户
	svc.noteRecordNotAvailable(chatReq, capture, nil)
	if code != "" || msg != "" {
		t.Fatalf("非归属冲突不应下发提示, got code=%q msg=%q", code, msg)
	}

	// 2. 归属冲突:必须下发 not_recorded 提示
	code, msg = "", ""
	svc.noteRecordNotAvailable(chatReq, capture, mstorage.ErrSessionOwnershipConflict)
	if code != cCommon.ChatNoticeNotRecorded || msg != cCommon.ConstChatNoticeNotRecordedMsg {
		t.Fatalf("归属冲突必须下发 not_recorded 提示, got code=%q msg=%q", code, msg)
	}

	// 3. 包装后的错误同样要被识别(errors.Is 语义)
	code = ""
	svc.noteRecordNotAvailable(chatReq, capture, fmt.Errorf("begin: %w", mstorage.ErrSessionOwnershipConflict))
	if code != cCommon.ChatNoticeNotRecorded {
		t.Fatalf("包装错误应被识别为归属冲突, got code=%q", code)
	}

	// 4. 同步接口没有 notice 通道:不得 panic(此时只落错误日志)
	svc.noteRecordNotAvailable(chatReq, nil, mstorage.ErrSessionOwnershipConflict)
}
