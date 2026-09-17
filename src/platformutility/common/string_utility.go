package common

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	NullString  = "<nil>" //表示nil字符串
	BlankString = ""      //空字符串
)

// EndsWithBlankLine 判断一段正文是否以空行(记录终止符)结尾。
func EndsWithBlankLine(s string) bool {
	trimmed := strings.TrimRight(s, "\n")
	return len(s) > 0 && len(s)-len(trimmed) >= 2
}

func ContainsString(text string, substrings []string) bool {
	for _, s := range substrings {
		if s != BlankString && strings.Contains(text, s) {
			return true
		}
	}

	return false
}

// LineSpace  链接两个字符串
var LineSpace = "- "

func ConcatStringWithLineSpace(s2 string) string {
	return LineSpace + s2
}

// NormalizeFact 事实的规范化形式:去空白、去标点差异、小写。
// 目的:同一事实的不同措辞(空格/标点/大小写)不应被重复写入。
func NormalizeFact(fact string) string {
	fact = strings.TrimSpace(fact)
	if fact == BlankString {
		return BlankString
	}
	var sb strings.Builder
	for _, r := range fact {
		switch r {
		case ' ', '\t', '。', '，', ',', '.', ';', '；', '!', '！', '?', '？', ':', '：', '、':
			continue
		default:
			sb.WriteRune(r)
		}
	}
	return strings.ToLower(sb.String())
}

// NormalizeBlankLine 收敛空行
func NormalizeBlankLine(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == BlankString {
			if blank {
				continue
			}
			blank = true
			out = append(out, BlankString)
			continue
		}
		blank = false
		out = append(out, line)
	}
	for len(out) > 0 && strings.TrimSpace(out[0]) == BlankString {
		out = out[1:]
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == BlankString {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n") + "\n"
}

// FactShingles 把一条事实规范化为"字符二元组"集合,用于近似重复/更新判定。
// 用字符二元组而不是分词:中文没有空格,二元组对"取值变了但句子没变"这种更新很敏感。
func FactShingles(fact string) map[string]struct{} {
	normalized := NormalizeFact(fact)
	shingles := make(map[string]struct{}, len(normalized))

	runes := []rune(normalized)
	if len(runes) < 2 {
		if len(runes) == 1 {
			shingles[string(runes)] = struct{}{}
		}
		return shingles
	}
	for i := 0; i+1 < len(runes); i++ {
		shingles[string(runes[i:i+2])] = struct{}{}
	}
	return shingles
}

// FactSimilarity 计算两条事实的字符二元组 Jaccard 相似度(0~1;任一侧为空返回 0)。
func FactSimilarity(a, b string) float64 {
	left, right := FactShingles(a), FactShingles(b)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}

	intersection := 0
	for shingle := range left {
		if _, ok := right[shingle]; ok {
			intersection++
		}
	}
	if intersection == 0 {
		return 0
	}

	union := len(left) + len(right) - intersection
	return float64(intersection) / float64(union)
}

// FactSame 在既有条目里找"同一槽位"的那条(找不到返回 -1)。
//
// 判定分两级:规范化相等(**与长度无关**)→ 高相似度。第二级有 minRunes 门槛
// (短句的相似度不可靠),因此只用相似度会把短条目反复当成"新事实"重复写入。
func FactSame(facts []string, fact string, ratio float64, minRunes int) int {
	key := NormalizeFact(fact)
	if key != BlankString {
		for i := range facts {
			if key == NormalizeFact(facts[i]) {
				return i
			}
		}
	}
	return FindSimilarFact(facts, fact, ratio, minRunes)
}

// IsSimilarFact 判定两条事实是否属于"同一件事的不同说法/同一槽位的取值更新"。
// 两侧都短于 minRunes 时一律返回 false:短句的相似度不可靠,宁可重复也不误合并。
func IsSimilarFact(a, b string, ratio float64, minRunes int) bool {
	if minRunes > 0 && (utf8.RuneCountInString(a) < minRunes || utf8.RuneCountInString(b) < minRunes) {
		return false
	}
	return FactSimilarity(a, b) >= ratio
}

// TruncateRunes 按 rune 安全截断,避免截断多字节字符;fast-path 直接按字节比较。
func TruncateRunes(s string, max int, marker string) string {
	if max <= 0 || len(s) <= max {
		return s
	}

	runes := []rune(s)
	if len(runes) <= max {
		return s
	}

	return string(runes[:max]) + marker
}

func TruncateString(s string, beginIndex, length int) string {
	sLen := len(s)
	if sLen == 0 || beginIndex >= sLen {
		return BlankString
	}

	if beginIndex < 0 {
		beginIndex = 0
	}

	if length > sLen-beginIndex {
		length = sLen - beginIndex
	}

	return s[beginIndex : beginIndex+length]
}

// IsCJK 判断字符是否属于中日韩(CJK)文字区。
// 这些字符在多数 tokenizer 中每字符约 0.5~1 token,与拉丁字符(约 4 字符/token)差异明显,
// 分开统计可让估算更贴近真实用量。
func IsCJK(r rune) bool {
	switch {
	case r >= 0x3400 && r <= 0x4DBF, // CJK 扩展 A
		r >= 0x4E00 && r <= 0x9FFF, // CJK 统一表意文字
		r >= 0x3040 && r <= 0x30FF, // 平假名/片假名
		r >= 0xAC00 && r <= 0xD7AF, // 谚文(Hangul)
		r >= 0xF900 && r <= 0xFAFF: // CJK 兼容表意文字
		return true
	default:
		return false
	}
}

// SplitString 把点分路径拆成段，并忽略空段（如 "a..b" 视为 "a.b"）。
func SplitString(dottedKey string) []string {
	raw := strings.Split(dottedKey, ".")
	segments := make([]string, 0, len(raw))
	for _, seg := range raw {
		seg = strings.TrimSpace(seg)
		if len(seg) > 0 {
			segments = append(segments, seg)
		}
	}
	return segments
}

func SplitKeyWords(keywords string) []string {
	return strings.FieldsFunc(keywords, func(r rune) bool {
		return r == ',' || r == '，'
	})
}

// ValueToString 把配置树节点转换为字符串。
// 对象/数组返回其 JSON 文本；nil 视为不存在（返回 false）;其余类型转换为字符串表示。
func ValueToString(v interface{}) (string, bool) {
	switch value := v.(type) {
	case nil:
		return BlankString, false
	case string:
		return value, true
	case bool:
		return strconv.FormatBool(value), true
	case json.Number:
		return value.String(), true
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(value), 'f', -1, 32), true
	case int:
		return strconv.Itoa(value), true
	case int64:
		return strconv.FormatInt(value, 10), true
	case uint64:
		return strconv.FormatUint(value, 10), true
	default:
		// map / slice / 其他自定义类型：序列化为 JSON 文本
		content, err := json.Marshal(value)
		if err != nil {
			return BlankString, false
		}
		return string(content), true
	}
}

// ToJson 把字符串值转换为 JSON 兼容类型：先识别 bool，再识别数字，否则按字符串处理。
// 数字以 json.Number 形式保存，保留原字面量精度。
func ToJson(value string) interface{} {
	value = strings.TrimSpace(value)
	switch value {
	case "true":
		return true
	case "false":
		return false
	}

	num := json.Number(value)
	if _, err := num.Float64(); err == nil {
		return num
	}
	return value
}

// UniqueStrings 按首次出现顺序去重:同一个 section 可能同时被单上限与总预算记录
func UniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	kept := make([]string, 0, len(values))

	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}

		seen[value] = struct{}{}
		kept = append(kept, value)
	}

	return kept
}

// FindSimilarFact 找出高度相似的项
func FindSimilarFact(facts []string, fact string, rate float64, minRunes int) int {
	for i := range facts {
		if IsSimilarFact(facts[i], fact, rate, minRunes) {
			return i
		}
	}
	return -1
}

// DedupeFacts 去掉一批输出内部的重复:精确规范化相同直接去重;
// 高相似(同一槽位的不同说法)保留**更长的**那条(信息更多)。
func DedupeFacts(items []string, rate float64, minRunes int) []string {
	kept := make([]string, 0, len(items))
	for _, item := range items {
		normalized := NormalizeFact(item)
		duplicated := false
		for i := range kept {
			if NormalizeFact(kept[i]) == normalized {
				duplicated = true
				break
			}
			if IsSimilarFact(kept[i], item, rate, minRunes) {
				if utf8.RuneCountInString(item) > utf8.RuneCountInString(kept[i]) {
					kept[i] = item
				}
				duplicated = true
				break
			}
		}
		if !duplicated {
			kept = append(kept, item)
		}
	}
	return kept
}

// LooksLikeFact 判断看起来是独立的行
func LooksLikeFact(item string, minFactRuns int) bool {
	if utf8.RuneCountInString(item) < minFactRuns {
		return false // 过短的多半是标签/残句
	}
	if strings.HasSuffix(item, ":") || strings.HasSuffix(item, "：") {
		return false // "以下是提炼出的长期记忆:" 这类前言
	}
	if strings.HasPrefix(item, ">") || strings.HasPrefix(item, "---") || strings.HasPrefix(item, "**") {
		return false // 引用/分隔线/加粗标题
	}
	return true
}

// LooksLikeJson 判断字符串是否是json串
func LooksLikeJson(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 2 {
		return false
	}

	head, tail := trimmed[0], trimmed[len(trimmed)-1]
	if !(head == '{' && tail == '}') && !(head == '[' && tail == ']') {
		return false
	}

	return json.Valid([]byte(trimmed))
}
