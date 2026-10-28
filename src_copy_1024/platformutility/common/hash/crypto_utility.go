/*
* 系统级通用的crypto工具函数
* 因为不需要维护任何状态,因此,定义函数即可实现
 */

package hash

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
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

// GenerateRandomString 生成完全随机的字符串
func GenerateRandomString() (string, error) {
	b := make([]byte, 32) // 32Bytes = 256 bit
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HmacSha256 HmacSha256 计算摘要
func HmacSha256(key, data string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func HmacEqual(originalData []byte, encryptedData []byte) bool {
	return hmac.Equal(originalData, encryptedData)
}

func LoadX509KeyPair(certFile, keyFile string) (cert tls.Certificate, err error) {
	cert, err = tls.LoadX509KeyPair(certFile, keyFile)
	return cert, err
}
