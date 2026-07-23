package core

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"my.favorite/platformutility/common"
)

type ImageCapabilities struct {
	Images       bool
	CellWidthPx  int32
	CellHeightPx int32
}

var capabilities = &ImageCapabilities{
	Images: common.GetEnvString("NO_IMAGE") != "1",
}

func GetImageCapabilities() *ImageCapabilities {
	return capabilities
}

func CellDimensions() (int, int) {
	return int(atomic.LoadInt32(&capabilities.CellHeightPx)), int(atomic.LoadInt32(&capabilities.CellWidthPx))
}

func SetCellDimensions(widthPx, heightPx int) {
	atomic.StoreInt32(&capabilities.CellWidthPx, int32(widthPx))
	atomic.StoreInt32(&capabilities.CellHeightPx, int32(heightPx))
}

// ========================= private functions & methods ====================================

func isImageLine(line string) bool {
	return strings.Contains(line, ImagePrefix)
}

// 生成删除图像的转义序列
func deleteImage(id int) string {
	return fmt.Sprintf("\x1b_Ga=d,d=A,i=%d,q=2\x1b\\", id)
}

// 批量删除图像
func deleteImages(ids []int) string {
	var b strings.Builder
	for _, id := range ids {
		b.WriteString(deleteImage(id))
	}

	return b.String()
}

func getImageReservedRows(lines []string, index, maxIndex int) int {
	rows := extractImageRows(lines[index])
	if rows <= 1 {
		return 1
	}

	maxRows := min(rows, maxIndex-index+1, len(lines)-index)
	reservedRows := 1

	for reservedRows < maxRows {
		line := lines[index+reservedRows]
		if isImageLine(line) || visibleWidth(line) > 0 {
			break
		}

		reservedRows++
	}

	return reservedRows
}

// handles CSI 6; height; width
func consumeCellSizeResponse(data string) (bool, int, int) {
	if !strings.HasPrefix(data, "\x1b[6;") || !strings.HasSuffix(data, "t") {
		return false, 0, 0
	}

	parts := strings.SplitN(data[2:len(data)-1], ";", 3)
	if len(parts) != 3 || parts[0] != "6" {
		return false, 0, 0
	}

	h, _ := strconv.Atoi(parts[1])
	w, _ := strconv.Atoi(parts[2])
	return true, h, w
}
