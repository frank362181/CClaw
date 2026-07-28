package exception

const (
	errorIdParamEmpty = iota + 1
	errorIdMatch
	errorIdUnMatch
	errorIdFileExtension

	errorIdUnsupported

	errorIdUnloadFile = iota + 60 //配置文件载入错误
	errorIdFilePath
	errorIdConfigRW // 配置文件读写时发生的错误编码

	errorIdDatabase
	errorIdSqlError
	errorIdMemoryDbError

	errorIdUnstartServer = iota + 100

	errorIdAddUsing             = iota + 300 //定义的服务器监听相关的错误
	errorIdSocketConnectTimeout              //socket链接超时
	errorIdSocketOverBuffer                  //socket读缓冲区溢出

	errorIdUnknown = 999
)
