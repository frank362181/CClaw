package common

import (
	"crypto/md5"
	"fmt"
)

type CryptoUtil struct {
}

var cryptoInstance = GetSingleton(func() *CryptoUtil {
	return &CryptoUtil{}
}).Instance()

func GetCryptoInstance() *CryptoUtil {
	return cryptoInstance
}

func (c *CryptoUtil) MD5(input string) string {
	if len(input) == 0 {
		return ""
	}

	return c.md5(input)
}

// =================================== private functions & methods =================================

func (c *CryptoUtil) md5(input string) string {
	h := md5.Sum([]byte(input))
	output := fmt.Sprintf("%x", h)
	return output
}
