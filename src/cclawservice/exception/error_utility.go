package exception

import (
	"errors"

	"my.favorite/platformutility/exception"
)

// MapStartupFileError 把启动文件相关的仓库层/平台错误转换为 agent 管理业务错误,
// 使 controller 只处理业务错误,不感知底层平台异常。
func MapStartupFileError(err error) error {
	switch {
	case errors.Is(err, exception.FilePathError):
		// 启动文件不在白名单/路径越界:视为参数错误,不暴露具体路径
		return AgentParameterErrEx("illegal startup file")
	case errors.Is(err, exception.ParameterEmptyError):
		return AgentInternalErrEx("agent service internal error")
	default:
		return err
	}
}
