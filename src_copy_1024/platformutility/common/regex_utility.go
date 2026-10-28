package common

import (
	"regexp"
	"strconv"
)

// GetIntFromRegex 返回一段字节中记录头序号的最大值;无匹配返回 0。
func GetIntFromRegex(regStr string, data []byte) int {
	if len(regStr) == 0 {
		return -1
	}

	regex := regexp.MustCompile(regStr)
	maxIndex := 0
	for _, match := range regex.FindAllSubmatch(data, -1) {
		if len(match) < 2 {
			continue
		}
		if n, err := strconv.Atoi(string(match[1])); err == nil && n > maxIndex {
			maxIndex = n
		}
	}
	return maxIndex
}

func ReplaceStringWithRegexp(input string, regExp *regexp.Regexp, replacing string) string {
	return regExp.ReplaceAllString(input, replacing)
}
