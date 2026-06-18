package logging

import (
	"fmt"
	"os"
	"sync"
)

type FileWriter struct {
	mu          sync.Mutex
	f           *os.File
	filePath    string
	maxSize     int64 //字节
	maxBackups  int
	currentSize int64
}

func (fw *FileWriter) Write(p []byte) (n int, err error) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	if fw.currentSize+int64(len(p)) >= fw.maxSize {
		if err := fw.rotate(); err != nil {
			return 0, err
		}
	}

	n, err = fw.f.Write(p)
	fw.currentSize += int64(n)
	return
}

func (fw *FileWriter) Close() error {
	if fw.f != nil {
		return fw.f.Close()
	}

	return nil
}

// =================================== private functions & methods ====================================
func (fw *FileWriter) openFile() error {
	file, err := os.OpenFile(fw.filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	fw.f = file
	info, _ := file.Stat()
	fw.currentSize = info.Size()
	return nil
}

func (fw *FileWriter) rotate() error {
	if fw.f != nil {
		err := fw.f.Close()
		if err != nil {
			return err
		}
	}

	// 轮转备份,删除最后一个,其余后移
	lastBackup := fmt.Sprintf("%s.%d", fw.filePath, fw.maxBackups)
	err := os.Remove(lastBackup)
	if err != nil {
		return err
	}

	for i := fw.maxBackups - 1; i >= 0; i-- {
		oldPath := fw.filePath
		if i > 0 {
			oldPath = fmt.Sprintf("%s.%d", fw.filePath, i)
		}

		newPath := fmt.Sprintf("%s.%d", fw.filePath, i+1)
		err := os.Rename(oldPath, newPath)
		if err != nil {
			return err
		}
	}

	return fw.openFile()
}
