//go:build windows

package heaven

import "syscall"

// setSocketOptions 设置 TCP 监听 socket 的 SO_REUSEADDR / SO_REUSEPORT。
// Windows 的 syscall.SetsockoptInt 第一个参数是 syscall.Handle（与 Unix 的 int 不同），
// 因此放在 windows 构建标签文件中，完整保留原有的 Windows 行为。
func setSocketOptions(fd uintptr) error {
	h := syscall.Handle(fd)
	if err := syscall.SetsockoptInt(h, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		return err
	}
	// SO_REUSEPORT = 15 (linux)；Windows 上维持原有实现
	return syscall.SetsockoptInt(h, syscall.SOL_SOCKET, 0xf, 1)
}
