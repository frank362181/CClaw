//go:build windows

package common

import "os"

// OpenFileNoFollow Windows 无 O_NOFOLLOW,降级为普通只读打开。
// 越界防护仍由 SafeJoin 的真实路径校验保证。
func OpenFileNoFollow(path string) (*os.File, os.FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}

	stat, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}

	return f, stat, nil
}
