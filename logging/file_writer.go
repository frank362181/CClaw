/*
@Summary:
	文件日志输出
@Date:
	18 Jun, 2026
@Author:
	Frank Ng
*/

package logging

import (
	"fmt"
	"os"
	"sync"
	"time"
)

type FileWriter struct {
	mu          sync.Mutex
	f           *os.File
	filePath    string
	maxSize     int64 //字节
	maxBackups  int
	currentSize int64

	currentDate string //当前日期
}

func (fw *FileWriter) Write(p []byte) (n int, err error) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	// 检查是否需要轮转
	if err := fw.checkAndRotate(); err != nil {
		return 0, err
	}

	n, err = fw.f.Write(p)
	fw.currentSize += int64(n)
	return n, err
}

func (fw *FileWriter) Close() error {
	if fw.f != nil {
		return fw.f.Close()
	}

	return nil
}

// =================================== private functions & methods ====================================
func (fw *FileWriter) openFile() error {
	if (fw.f != nil) && (fw.f != os.Stderr) {
		// 关闭旧文件,如果存在
		err := fw.f.Close()
		if err != nil {
			return err
		}
	}

	file, err := os.OpenFile(fw.filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	fw.f = file
	info, _ := file.Stat()
	fw.currentSize = info.Size()
	return nil
}

func (fw *FileWriter) checkAndRotate() error {
	today := time.Now().Format("2006-01-02") //获取当前日期

	//判断日期是否有变化
	if today == fw.currentDate {
		return nil
	}

	// 日期已经变化,执行轮转
	return fw.rotate()
}

func (fw *FileWriter) rotate() error {
	// 关闭当前的文件
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
