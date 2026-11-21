package npa

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	pCommon "my.favorite/platformutility/common"
)

// GenerateDocumentNo 根据项目的目录产生档案编号的同时,创建报告存放的目录
func GenerateDocumentNo(projectDir string, prefix string) string {
	datePart := time.Now().Format("20060102") // 日期格式化
	projectDir = pCommon.ResolveRealPath(projectDir)
	prefix = strings.TrimSpace(prefix)

	if projectDir == "" {
		return fmt.Sprintf("%s-%s-%s", prefix, datePart, randomSuffix())
	}

	for attempt := 0; attempt < 8; attempt++ {
		candidate := fmt.Sprintf("%s-%s-%s", prefix, datePart, randomSuffix())

		err := os.Mkdir(filepath.Join(projectDir, candidate), 0o755)
		if err == nil {
			return candidate
		}

		// 目录创建不成功(权限/父目录不存在)时不能无限次重复:回退到纯随机状态
		if !errors.Is(err, fs.ErrExist) {
			return candidate
		}
	}

	return fmt.Sprintf("%s-%s-%d", prefix, datePart, pCommon.GetDatetimeUtilityInstance().GetNow().UnixNano())
}

const documentNoRandLen = 6
const documentNoAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func randomSuffix() string {
	buf := make([]byte, documentNoRandLen)
	if _, err := rand.Read(buf); err != nil {
		fmt.Printf("%06d", pCommon.GetDatetimeUtilityInstance().GetNow().UnixNano()%1000000)
	}

	out := make([]byte, documentNoRandLen)
	for i, b := range buf {
		out[i] = documentNoAlphabet[int(b)%len(documentNoAlphabet)]
	}

	return string(out)
}
