/**
* 定义所有的内存数据库所需的类型定义
**/

package common

import "my.favorite/platformutility/config"

// MemoryDbClient 内存数据库链接的客户端
type MemoryDbClient struct {
	MConfig config.DataStorageConfig
}
