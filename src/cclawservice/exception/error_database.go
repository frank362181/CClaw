/*
* 数据库访问层的错误定义(单一事实来源)。
*
* 属基础设施类错误:不对外暴露业务码(Code=0),响应回退平台通用 500 码与文案;
* 细节(detail)只进日志,便于排障。
 */

package exception

import (
	"net/http"

	"my.favorite/cclawservice/apperr"
	"my.favorite/platformutility/webbase/middleware/basic"
)

// codeDatabase database 域业务码基址:ConstErrorMax + 1400 = 3400(域内自 +1 起)。
const codeDatabase = basic.ConstErrorMax + 1400

const (
	codeDatabaseQueryError = codeDatabase + 1 + iota // 3401
)

// DatabaseKind 数据库访问失败(gorm 执行/连接错误)。
var DatabaseKind = apperr.Register(apperr.Kind{
	Name:       "database.query",
	Code:       codeDatabaseQueryError,
	HTTPStatus: http.StatusInternalServerError,
	Msg:        basic.ConstInternalServerErrorMsg,
})

// ===== 兼容层:保持既有标识符与工厂签名,调用点无需改动 =====

// DatabaseQueryErr 数据库查询错误的哨兵(errors.Is 用)。
var DatabaseQueryErr = DatabaseKind.Sentinel()

func DatabaseQueryErrEx(errTxt string) *apperr.Error {
	return DatabaseKind.New(errTxt)
}

func DatabaseQueryErrEx2(errTxt string, a ...interface{}) *apperr.Error {
	return DatabaseKind.Newf(errTxt, a...)
}
