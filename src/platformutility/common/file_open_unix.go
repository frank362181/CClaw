//go:build !windows

package common

import (
	"os"
	"syscall"
)

// OpenFileNoFollow 以 O_NOFOLLOW 打开启动文件(Unix),
// 防止文件在校验后被替换为符号链接(TOCTOU)。返回句柄与文件信息。
func OpenFileNoFollow(path string) (*os.File, os.FileInfo, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
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
