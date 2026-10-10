/**
* 聊天历史存储接口类，所有的支持的记忆存储类都从该类即成
* 目前仅支持：
*	md 文件
**/

package ctxmgr

import (
	pCommon "my.favorite/platformutility/common"
)

type MemoryStorageType int

// 记忆存储枚举类型
const (
	MemoryStorageMDFile = 1 + iota
	MemoryStorageDB
)

type IMemoryStorage interface {
	StorageType() MemoryStorageType

	Ensure(sender, sessionId string, at *pCommon.DatetimeItem, headerLines []string) (string, error)
	Append(sender, sessionId, fileName, recordType, title, content, timeText string, hintIndex int) (int, error)

	// Read 读回某会话历史文件的全部记录(按序号升序);文件不存在返回 (nil, nil)。
	// 返回的切片是内部缓存,调用方只读。
	Read(sender, sessionId, fileName string) ([]MemoryRecord, error)

	// ForgetSession 丢弃某会话的进程内缓存(序号分配 + 读回快照)。
	// 删掉磁盘上的历史文件后必须一并丢弃缓存,否则复用的同号会话会从缓存序号继续分配。
	ForgetSession(sender, sessionId string)
}

type MemoryStorage struct {
	storageType MemoryStorageType
}

func (ms *MemoryStorage) StorageType() MemoryStorageType {
	return ms.storageType
}
