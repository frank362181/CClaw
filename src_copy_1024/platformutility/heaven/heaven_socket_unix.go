//go:build !windows

package heaven

import "syscall"

// setSocketOptions 设置 TCP 监听 socket 的 SO_REUSEADDR / SO_REUSEPORT。
// Unix(darwin/linux) 的 syscall.SetsockoptInt 第一个参数是 int（文件描述符），
// 并且 SO_REUSEPORT 在各平台取值不同（linux=15, darwin=0x200），必须使用命名常量。
func setSocketOptions(fd uintptr) error {
	if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		return err
	}
	return syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1)
}
