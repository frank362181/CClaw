//go:build !windows

package common

import (
	"os"
	"os/exec"
	"syscall"
)

// OpenFileNoFollow 以 O_NOFOLLOW 打开启动文件(Unix),
// 同时加 O_NONBLOCK:调用方"先判类型再打开"时,Lstat 与 Open 之间存在窗口,
// 若其间被换成 FIFO/设备节点,O_RDONLY 打开会**阻塞到有写端**(实测确认)。
// O_NONBLOCK 让该窗口内的打开立即返回,调用方再用 Mode 判定拒绝即可。
// 对普通文件 O_NONBLOCK 无副作用(POSIX 明确不生效,读取仍是同步返回)。
func OpenFileNoFollow(path string) (*os.File, os.FileInfo, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}

	f := os.NewFile(uintptr(fd), path)
	if f == nil {
		_ = syscall.Close(fd)
		return nil, nil, os.ErrInvalid
	}

	// fstat 打开句柄,避免二次按路径 stat 引入 TOCTOU
	stat, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}

	return f, stat, nil
}

func SetProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func KillProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		return
	}

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		// 拿不到组号:回退只kill sub-process
		_ = cmd.Process.Kill()
		return
	}

	if pgid <= 1 {
		_ = cmd.Process.Kill()
		return
	}

	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}
