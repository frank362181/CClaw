/*
* 定义security所需的所有工具类函数
**/

package security

import "strings"

// MatchesTool 判断工具是否匹配,通配符 * 表示匹配
func MatchesTool(pattern, toolName string) bool {
	return strings.EqualFold(pattern, toolName) || pattern == "*"
}
