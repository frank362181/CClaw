package hash

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"

	"my.favorite/platformutility/exception"
)

func MD5(input string) string {
	h := md5.Sum([]byte(input))
	output := fmt.Sprintf("%x", h)
	return output
}

func SHA256(input string) (string, error) {
	input = strings.TrimSpace(input)
	if len(input) == 0 {
		return "", exception.ParameterEmptyErrorEx("text is empty")
	}

	h := sha256.Sum256([]byte(input))
	return base64.StdEncoding.EncodeToString(h[:]), nil
}

func SHA256Multi(texts ...string) (string, error) {
	if len(texts) == 0 {
		return "", exception.ParameterEmptyErrorEx("text is empty")
	}

	stringStrings := strings.Join(texts, "")
	h := sha256.Sum256([]byte(stringStrings))
	return base64.StdEncoding.EncodeToString(h[:]), nil
}

// =================================== private functions & methods =================================
