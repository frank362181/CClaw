//go:build !windows

/**
* OpenFileNoFollow 的"非阻塞打开"回归测试。
*
* 动机:调用方(ReadDocFile)采用"先 Lstat 判类型,再 Open 读取"的写法。
* 若 Lstat 与 Open 之间文件被换成 FIFO/设备节点,裸 O_RDONLY 打开会阻塞到有写端 ——
* 实测确认这是真实阻塞点。O_NONBLOCK 让该窗口内的打开立即返回。
**/

package common

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestOpenFileNoFollowDoesNotBlockOnFifo 锁定:FIFO 上打开必须立即返回(不得阻塞)。
func TestOpenFileNoFollowDoesNotBlockOnFifo(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}

	type result struct {
		f     *os.File
		info  os.FileInfo
		err   error
		state string
	}
	ch := make(chan result, 1)
	go func() {
		f, info, err := OpenFileNoFollow(fifo)
		ch <- result{f: f, info: info, err: err}
	}()

	select {
	case r := <-ch:
		if r.f != nil {
			defer func() { _ = r.f.Close() }()
		}
		if r.info != nil && r.info.Mode().IsRegular() {
			t.Errorf("FIFO 不应被判为普通文件")
		}
		t.Logf("FIFO 打开立即返回: err=%v (调用方据此按非普通文件拒绝)", r.err)
	case <-time.After(2 * time.Second):
		// 解阻塞,防 goroutine 泄漏
		if w, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = w.Close()
		}
		t.Fatal("OpenFileNoFollow 在 FIFO 上阻塞 -> O_NONBLOCK 未生效")
	}
}

// TestOpenFileNoFollowSwapWindow 直接复现残留窗口:
// Lstat 判定为普通文件之后、Open 之前被替换为 FIFO —— 打开仍必须立即返回。
func TestOpenFileNoFollowSwapWindow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("regular\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 第 1 步:类型校验(此时是普通文件)
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatal("前置条件:应为普通文件")
	}

	// 第 2 步:窗口内替换为 FIFO
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}

	// 第 3 步:打开(必须立即返回;由调用方二次 Mode 校验拒绝)
	ch := make(chan error, 1)
	go func() {
		f, _, err := OpenFileNoFollow(path)
		if f != nil {
			_ = f.Close()
		}
		ch <- err
	}()

	select {
	case err := <-ch:
		t.Logf("窗口内替换为 FIFO 后,打开立即返回: err=%v", err)
	case <-time.After(2 * time.Second):
		if w, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = w.Close()
		}
		t.Fatal("Lstat→Open 窗口内被换成 FIFO 时仍阻塞 -> 残留窗口未闭合")
	}
}

// TestOpenFileNoFollowRegularFileUnaffected 锁定 O_NONBLOCK 对普通文件无副作用。
func TestOpenFileNoFollowRegularFileUnaffected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.md")
	want := make([]byte, 256*1024)
	for i := range want {
		want[i] = byte('a' + i%26)
	}
	if err := os.WriteFile(path, want, 0o644); err != nil {
		t.Fatal(err)
	}

	f, info, err := OpenFileNoFollow(path)
	if err != nil {
		t.Fatalf("普通文件应可打开: %v", err)
	}
	defer func() { _ = f.Close() }()

	if !info.Mode().IsRegular() {
		t.Fatal("应为普通文件")
	}
	if info.Size() != int64(len(want)) {
		t.Errorf("大小不符: got=%d want=%d", info.Size(), len(want))
	}

	got := make([]byte, len(want)+1)
	n, err := f.Read(got)
	if err != nil && err.Error() != "EOF" {
		t.Fatalf("读取普通文件不应因 O_NONBLOCK 失败: %v", err)
	}
	if string(got[:n]) != string(want) {
		t.Errorf("读取内容不一致: got=%d bytes", n)
	}
}
