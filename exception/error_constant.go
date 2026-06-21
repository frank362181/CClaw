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

	errorIdUnknown = 999
)
