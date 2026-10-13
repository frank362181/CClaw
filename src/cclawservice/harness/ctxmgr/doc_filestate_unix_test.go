//go:build !windows

/**
* 非普通文件(目录/FIFO/设备节点)的读盘守卫测试(仅 Unix:需要 mkfifo)。
*
* 背景:FIFO 的 Size() 为 0,能通过 ReadDocFile 的超限预检;若不挡文件类型,
* io.ReadAll 会在 O_RDONLY 打开后**阻塞到有写端出现**。
* 而 Snapshot ← LoadAgentBootstrap 是**每轮对话**都会走的路径,
* 一个名为 AGENTS.md 的 FIFO 足以挂死每轮 prompt 装配(该路径无独立超时)。
**/

package ctxmgr

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	hCommon "my.favorite/cclawservice/harness/common"
)

const utNoBlockTimeout = 2 * time.Second

// utMkfifo 建一个 FIFO;失败则跳过(平台/权限不支持)。
func utMkfifo(t *testing.T, path string) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}
}

// utUnblockFifo 给 FIFO 挂一个写端再立即关闭,解除可能阻塞的读端(防 goroutine 泄漏)。
func utUnblockFifo(path string) {
	if f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
		_ = f.Close()
	}
}

// TestReadDocFileRejectsNonRegular 锁定:非普通文件必须被拒绝,且**立即**返回(不得进入读取)。
func TestReadDocFileRejectsNonRegular(t *testing.T) {
	dir := t.TempDir()

	// 目录:按缺失
	if snap := ReadDocFile(dir, 1024); snap.State != FileMissing {
		t.Errorf("目录应按缺失: State=%v reason=%q", snap.State, snap.Reason)
	}

	// FIFO:Size()==0 能过超限预检,必须靠类型守卫拦住
	fifo := filepath.Join(dir, "pipe.md")
	utMkfifo(t, fifo)

	done := make(chan DocFileSnapshot, 1)
	go func() { done <- ReadDocFile(fifo, 1024) }()

	select {
	case snap := <-done:
		if snap.State == FileReady {
			t.Errorf("FIFO 不得被判为可读: %+v", snap)
		}
		if snap.Reason == "" {
			t.Error("拒绝非普通文件时应给出原因")
		}
		t.Logf("FIFO 被正确拒绝: State=%v reason=%q", snap.State, snap.Reason)
	case <-time.After(utNoBlockTimeout):
		utUnblockFifo(fifo)
		t.Fatal("ReadDocFile 在 FIFO 上阻塞 -> 未挡住非普通文件")
	}

	utUnblockFifo(fifo)
}

// TestStartupPathsRejectFifo 端到端锁定:工作区里名为 AGENTS.md 的 FIFO
// 不得让批量快照与单文件读取挂住 —— 两者都必须按"缺失"处理并立即返回。
func TestStartupPathsRejectFifo(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, hCommon.AgentDocFileName)
	utMkfifo(t, fifo)

	repo := GetAgentFileRepository()

	type snapshotResult struct {
		snap *StartupSnapshot
		err  error
	}
	ch := make(chan snapshotResult, 1)
	go func() {
		snap, err := repo.Snapshot(root)
		ch <- snapshotResult{snap, err}
	}()

	var snap *StartupSnapshot
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("Snapshot: %v", r.err)
		}
		snap = r.snap
	case <-time.After(utNoBlockTimeout):
		utUnblockFifo(fifo)
		t.Fatal("Snapshot 在 FIFO 上阻塞 -> 每轮 prompt 装配会被挂死")
	}

	for i := range snap.Files {
		if snap.Files[i].Spec.Name == hCommon.AgentDocFileName && snap.Files[i].State != FileMissing {
			t.Errorf("FIFO 形式的 AGENTS.md 应按缺失: State=%v", snap.Files[i].State)
		}
	}

	// 管理清单同样按缺失(不携带内容)
	for _, doc := range repo.GetStartupFiles(root) {
		if doc.FileName == hCommon.AgentDocFileName && (!doc.Missing || doc.ByteSize != 0) {
			t.Errorf("清单应把 FIFO 记为缺失且无大小: %+v", doc)
		}
	}

	// 单文件读取:必须立即返回"缺失",不得阻塞
	fileDone := make(chan *hCommon.AgentDocument, 1)
	go func() {
		doc, _ := repo.GetStartupFile(root, hCommon.AgentDocFileName)
		fileDone <- doc
	}()
	select {
	case doc := <-fileDone:
		if doc == nil || !doc.Missing {
			t.Errorf("单文件读取应返回缺失占位: %+v", doc)
		}
	case <-time.After(utNoBlockTimeout):
		utUnblockFifo(fifo)
		t.Fatal("GetStartupFile 在 FIFO 上阻塞")
	}

	utUnblockFifo(fifo)
}
