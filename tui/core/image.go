package core

import (
	"strconv"
	"strings"
)

// ImageHeader 图像传输头信息
type ImageHeader struct {
	ids  []int //图像ID列表
	rows int   //图像占用的行数
}

// 解析图像头
func parseImageHeader(line string) *ImageHeader {
	idx := strings.Index(line, ImagePrefix)
	if idx == -1 {
		return nil
	}

	start := idx + len(ImagePrefix)
	end := strings.Index(line[start:], ";")
	if end == -1 {
		return nil
	}
	end += start
	params := line[start:end]
	h := &ImageHeader{
		ids:  make([]int, 0),
		rows: 1,
	}

	for _, p := range strings.Split(params, ",") {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			continue
		}

		v, err := strconv.Atoi(kv[1])
		if err != nil || v <= 0 || v > 0xFFFFFFFF {
			continue
		}

		switch kv[0] {
		case "i":
			h.ids = append(h.ids, v)
		case "r":
			h.rows = v
		}
	}

	return h
}

// 提取行中的图像ID
func extractImageIDs(line string) []int {
	if h := parseImageHeader(line); h != nil {
		return h.ids
	}

	return nil
}

// 提取图像占用的行数
func extractImageRows(line string) int {
	if h := parseImageHeader(line); h != nil {
		return h.rows
	}

	return 1
}
