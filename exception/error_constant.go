package exception

const (
	errorIdParamEmpty = iota + 1
	errorIdMatch
	errorIdUnMatch
	errorFileExtension

	errorUnsupported

	errorUnloadFile = 60 //配置文件载入错误

	errorUnknown = 999
)
