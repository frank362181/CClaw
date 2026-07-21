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

type TerminatorColorSchema string

const (
	SchemaDark  TerminatorColorSchema = "dark"
	SchemaLight TerminatorColorSchema = "light"
)

func isOsc11Response(data string) bool {
	return strings.HasPrefix(data, "\x1b]11")
}

func parseOsc11Response(data string) (RGBColor, bool) {
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

func parseColorSchemaReport(data string) (TerminatorColorSchema, bool) {
	if strings.HasPrefix(data, "\x1b[?997;") && strings.HasSuffix(data, "n") {
		if strings.Contains(data, ";1") {
			return SchemaDark, true
		}

		if strings.Contains(data, ";2") {
			return SchemaLight, true
		}
	}

	return "", false
}
