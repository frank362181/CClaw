/*
* 这里定义的是全局通用的错误ID
*
* 编码规则：按业务域分段，每段内连续递增（每段独立 const 块，iota 从 0 重新计数）：
*   0 段   ：全局基础错误
*   100 段 ：参数 / 匹配 / 文件 / 配置 通用错误
*   300 段 ：数据库错误
*   500 段 ：服务器监听 / 网络 / Web 错误
*   2000 段：系统级错误
 */

package exception

// 全局基础错误（0 段）
const (
	errorIdBase    = iota // 0
	ErrorIdUnknown        // 1
)

// 参数 / 匹配 / 文件 / 配置 通用错误（100 段，连续）
const (
	ErrorIdSysParamEmpty            = iota + 100 // 100 参数为空
	ErrorIdSysMatch                              // 101 匹配成功
	ErrorIdSysUnMatch                            // 102 匹配失败
	ErrorIdSysFileExtension                      // 103 文件扩展名错误
	ErrorIdSysUnsupported                        // 104 不支持的操作
	ErrorIdSysStringJsonTranslation              // 105 字符串 JSON 转换错误
	ErrorIdSysUnloadFile                         // 106 文件载入错误
	ErrorIdSysFilePath                           // 107 文件路径错误
	ErrorIdSysConfigRW                           // 108 配置文件读写错误
	ErrorIdUnInitialized                         // 109 未初始化错误
)

const (
	ErrorIdSysParamEmptyTxt            = "参数为空"
	ErrorIdSysMatchTxt                 = "已经存在"
	ErrorIdSysUnMatchTxt               = "不存在"      // 102 匹配失败
	ErrorIdSysFileExtensionTxt         = "文件扩展名错误"  // 103 文件扩展名错误
	ErrorIdSysUnsupportedTxt           = "未支持"      // 104 不支持的操作
	ErrorIdSysStringJsonTranslationTxt = "JSON转换错误" // 105 字符串 JSON 转换错误
	ErrorIdSysUnloadFileTxt            = "文件载入错误"   // 106 文件载入错误
	ErrorIdSysFilePathTxt              = "文件路径错误"   // 107 文件路径错误
	ErrorIdSysConfigRWTxt              = "配置文件读取错误" // 108 配置文件读写错误
	ErrorIdUnInitializedTxt            = "未初始化"
)

// 数据库错误（300 段，连续）
const (
	ErrorIdSysDatabase      = iota + 300 // 300 数据库错误
	ErrorIdSysSqlError                   // 301 SQL 错误
	ErrorIdSysMemoryDbError              // 302 内存数据库错误
)

const (
	ErrorIdSysDatabaseTxt      = "数据库错误"
	ErrorIdSysSqlErrorTxt      = "SQL语法错误" // 301 SQL 错误
	ErrorIdSysMemoryDbErrorTxt = "内存数据库错误"
)

// 服务器监听 / 网络 / Web 错误（500 段，连续）
const (
	ErrorIdSysAddUsing             = iota + 500 // 服务器监听相关错误
	ErrorIdSysSocketConnectTimeout              // socket 链接超时
	ErrorIdSysSocketOverBuffer                  // socket 读缓冲区溢出
	ErrorIdSysWebServer                         // Web 服务器错误
)

const (
	ErrorIdSysSocketConnectTimeoutTxt = "socket链接超时"      // socket 链接超时
	ErrorIdSysSocketOverBufferTxt     = "socket buffer溢出" // socket 读缓冲区溢出
	ErrorIdSysWebServerTxt            = "web 服务器错误"
)

// ErrorIdSystemBase 系统级错误（2000 段）业务系统的错误从该段往后延续
const (
	ErrorIdSystemBase = iota + 2000 // 2000 系统级错误基址
)
