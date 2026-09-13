/**
* 聊天历史存储接口类，所有的支持的记忆存储类都从该类即成
* 目前仅支持：
*	md 文件
**/

package mstorage

import pCommon "my.favorite/platformutility/common"

type MemoryStorageType int

// 记忆存储枚举类型
const (
	MemoryStorageMDFile = 1 + iota
	MemoryStorageDB
)

type IMemoryStorage interface {
	StorageType() MemoryStorageType

	MemoryDir(sessionId string) (string, error)
	Ensure(sessionId string, at *pCommon.DatetimeItem, headerLines []string) (string, error)
	Append(sessionId, fileName, recordType, title, content, timeText string, hintIndex int) (int, error)
}

type MemoryStorage struct {
	storageType MemoryStorageType
}

func (ms *MemoryStorage) StorageType() MemoryStorageType {
	return ms.storageType
}
