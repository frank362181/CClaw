/*
* 用来记录日志
* 一般设置成第一个
 */

package filter

import (
	"fmt"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

var loggingFilter = common.GetSingleton(func() *LoggingFilter {
	return &LoggingFilter{
		BaseFilter: basic.BaseFilter{
			Order:      basic.FilterOrderLogging,
			FilterName: fmt.Sprintf("loggingFilter_%d", basic.FilterOrderLogging),
		},
	}
}).Instance()

func GetLoggingFilter() *LoggingFilter {
	return loggingFilter
}
