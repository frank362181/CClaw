/*
*  responsibility for the color
 */

package core

import (
	"strconv"
	"strings"
)

type RGBColor struct {
	R, G, B uint8
}

// 检查是否为OSC 11 背景色响应
func isOsc11BackgroundResponse(data string) bool {
	return strings.HasPrefix(data, "\x1b]11")
}

func parseOsc11BackgroundResponse(data string) (RGBColor, bool) {
	// format ESC 11 ]; rgb: RR/GG/BB
	parts := strings.Split(data, ";")
	if len(parts) < 3 {
		return RGBColor{}, false
	}

	rgbPart := strings.TrimPrefix(parts[len(parts)-1], "\x07")
	rgbPart = strings.TrimPrefix(rgbPart, "\x1b\\")
	rgbPart = strings.TrimPrefix(rgbPart, "rgb:")
	comps := strings.Split(rgbPart, "/")
	if len(comps) != 3 {
		return RGBColor{}, false
	}

	r, _ := strconv.ParseUint(comps[0], 16, 8)
	g, _ := strconv.ParseUint(comps[1], 16, 8)
	b, _ := strconv.ParseUint(comps[2], 16, 8)

	return RGBColor{uint8(r), uint8(g), uint8(b)}, true
}

// 解析颜色报告方案
func parseColorSchemaReport(data string) (TerminatorColorSchema, bool) {
	if !strings.HasPrefix(data, "\x1b[?997;") || !strings.HasSuffix(data, "n") {
		return SchemaUnknown, false
	}

	body := data[6 : len(data)-1]
	parts := strings.Split(body, ";")
	if len(parts) != 2 {
		return SchemaUnknown, false
	}

	switch parts[1] {
	case "1":
		return SchemaDark, true
	case "2":
		return SchemaLight, true
	}

	return SchemaUnknown, false
}
