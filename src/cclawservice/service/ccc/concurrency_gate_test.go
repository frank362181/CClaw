/**
* ConcurrencyGate(流式/全局聊天闸)单元测试:上限拒绝、释放后放行、SetMax 生效。
**/

package ccc

import "testing"

func TestConcurrencyGate_LimitAndRelease(t *testing.T) {
	g := &ConcurrencyGate{max: 2}
	if !g.TryAcquire() || !g.TryAcquire() {
		t.Fatal("should acquire 2 slots within limit")
	}
	if g.TryAcquire() {
		t.Fatal("should be rejected when limit reached")
	}
	g.Release()
	if !g.TryAcquire() {
		t.Fatal("should acquire after release")
	}
	if g.Active() != 2 {
		t.Fatalf("active = %d, want 2", g.Active())
	}
}

func TestConcurrencyGate_SetMax(t *testing.T) {
	g := &ConcurrencyGate{max: 5}
	g.TryAcquire()
	g.setMax(1) // 已占用 1,等于新上限:再取应失败
	if g.TryAcquire() {
		t.Fatal("should be rejected after shrink to active")
	}
	g.Release()
	if !g.TryAcquire() {
		t.Fatal("should acquire after release below new limit")
	}
}

// TestChatConcurrencyGate_IndependentInstance 验证全局聊天闸与流式闸是两个独立实例。
func TestChatConcurrencyGate_IndependentInstance(t *testing.T) {
	stream := GetStreamConcurrencyGate(2)
	chat := GetChatConcurrencyGate(1)

	if stream == chat {
		t.Fatal("stream gate and chat gate should be distinct instances")
	}
	if !chat.TryAcquire() || chat.TryAcquire() {
		t.Fatal("chat gate should follow its own limit(=1)")
	}
	if !stream.TryAcquire() {
		t.Fatal("stream gate should be unaffected by chat gate")
	}
	chat.Release()
	stream.Release()
}
