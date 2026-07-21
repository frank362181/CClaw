package core

import (
	"fmt"
	"strconv"
	"strings"

	"my.favorite/platformutility/common"
)

var capabilities = struct{ images bool }{images: common.GetEnvString("NO_IMAGE") != "1"}

func hasImageCapability() bool {
	return capabilities.images
}

func setCellDimensions(widthPx, heightPx int) {

}

func isImageLine(line string) bool {
	return strings.Contains(line, ImagePrefix)
}

func deleteImage(id int) string {
	return fmt.Sprintf("\x1b_Ga=d,d=A,i=%d,q=2\x1b\\", id)
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
