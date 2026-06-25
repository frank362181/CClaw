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

func GetLoggingFilter(order int) *LoggingFilter {
	loggingFilter := common.GetSingleton(func() *LoggingFilter {
		return &LoggingFilter{
			BaseFilter: basic.BaseFilter{
				Order:      order,
				FilterName: fmt.Sprintf("loggingFilter_%d", order),
			},
		}
	}).Instance()

	return loggingFilter
}
