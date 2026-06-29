/*
* 系统级通用的crypto工具函数
* 因为不需要维护任何状态,因此,定义函数即可实现
 */

package hash

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"

	"my.favorite/platformutility/common"
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

// GenerateSSLCertFile ===== 这个函数是用来专门生成一个SSL所需的文件
func GenerateSSLCertFile(domainName string) bool {
	domainName = strings.ToLower(domainName)
	if len(domainName) == 0 {
		return false
	}

	domainName = strings.ToLower(domainName) //全部转换成小写

	//生成 ECDSA私钥
	privateKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	//cert template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"cclaw"},
			CommonName:   domainName,
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(1, 0, 0),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{
			net.ParseIP("127.0.0.1"),
			net.ParseIP("::1"), // IPv6
		},
		DNSNames: []string{domainName},
	}

	//自签名
	certDer, _ := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)

	//写入私钥文件
	sslPath := common.GetOSUtilityInstance().GetCurrentDir() + "\\cert"
	_ = common.CreateDir(sslPath)
	keyFileFileName := sslPath + "\\" + "key.pem"
	keyFile, err := common.CreateFile(keyFileFileName)
	if err != nil {
		return false
	}
	defer common.Close(keyFile)

	privateBytes, _ := x509.MarshalPKCS8PrivateKey(privateKey)
	err = pem.Encode(keyFile, &pem.Block{Type: "SSL PRIVATE KEY", Bytes: privateBytes})
	if err != nil {
		return false
	}

	// === 写证书问津
	certFileName := sslPath + "\\" + "cert.pem"
	certFile, err := common.CreateFile(certFileName)
	if err != nil {
		return false
	}
	defer common.Close(certFile)
	err = pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDer})
	if err != nil {
		return false
	}

	return true
}

// =================================== private functions & methods =================================
