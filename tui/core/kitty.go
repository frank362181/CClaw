package core

import (
	"strconv"
	"strings"
)

const kittyPrefix = "\x1b_G"

type kittyHeader struct {
	ids  []int
	rows int
}

func parseKittyHeader(line string) *kittyHeader {
	idx := strings.Index(line, kittyPrefix)
	if idx == -1 {
		return nil
	}

	start := idx + len(kittyPrefix)
	end := strings.Index(line[start:], ";")
	if end == -1 {
		return nil
	}
	end += start
	params := line[start:end]
	h := &kittyHeader{rows: 1}

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

func extractKittyIDs(line string) []int {
	if h := parseKittyHeader(line); h != nil {
		return h.ids
	}

	return nil
}

func extractKittyRows(line string) int {
	if h := parseKittyHeader(line); h != nil {
		return h.rows
	}

	return 1
}
