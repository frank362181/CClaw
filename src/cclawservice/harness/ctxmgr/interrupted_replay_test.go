package ctxmgr

import (
	"strings"
	"testing"

	hCommon "my.favorite/cclawservice/harness/common"
)

// TestAssembleContextMarksInterruptedReply 锁住"被中断的助手回复必须带残缺标记"这一契约。
//
// 为什么必须有标记:中断时写进历史的是**残缺**内容,不标注模型会把它当成完整发言
// (接着半句话继续说、或以为已经回答过而不再补充)。压缩/蒸馏侧的 recordLabel 已有
// "assistant(被中断)"标签,装配侧必须与它保持同一语义(见 interrupt-memory-chain.md M2)。
func TestAssembleContextMarksInterruptedReply(t *testing.T) {
	records := []hCommon.MemoryRecord{
		{Index: 1, RecordType: hCommon.HistoryRecordTypeUser, Content: "帮我分析这份材料"},
		{Index: 2, RecordType: hCommon.HistoryRecordTypeAssistant, Title: hCommon.HistoryRecordTypeInterrupted, Content: "好的,我先读取材料"},
		{Index: 3, RecordType: hCommon.HistoryRecordTypeUser, Content: "继续"},
		{Index: 4, RecordType: hCommon.HistoryRecordTypeAssistant, Content: "材料已分析完毕"},
	}

	result := AssembleContext("sys", records, "继续吗", hCommon.TurnContext{}, hCommon.ContextPolicy{})

	interruptedReplayed, normalReplayed := false, false
	for _, msg := range result.Messages {
		if msg.Role != hCommon.RoleAssistant {
			continue
		}

		if strings.Contains(msg.Content, "我先读取材料") {
			interruptedReplayed = true
			if !strings.HasPrefix(msg.Content, hCommon.InterruptedReplyNotice) {
				t.Fatalf("被中断的助手回复缺少残缺标记: %q", msg.Content)
			}
		}
		if strings.Contains(msg.Content, "材料已分析完毕") {
			normalReplayed = true
			if strings.Contains(msg.Content, hCommon.InterruptedReplyNotice) {
				t.Fatalf("正常助手回复不应带残缺标记: %q", msg.Content)
			}
		}
	}

	if !interruptedReplayed {
		t.Fatalf("被中断的助手记录没有被回放: %v", result.Messages)
	}
	if !normalReplayed {
		t.Fatalf("正常助手记录没有被回放: %v", result.Messages)
	}
}
