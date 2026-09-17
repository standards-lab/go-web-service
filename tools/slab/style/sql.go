package style

import "strings"

// sqlKeywords is the hand-maintained list SQL bolds. Match is exact: the
// repository's statements write keywords in upper case.
var sqlKeywords = map[string]bool{
	"SELECT": true, "FROM": true, "WHERE": true, "INSERT": true, "UPDATE": true,
	"DELETE": true, "SET": true, "JOIN": true, "ON": true, "AND": true, "OR": true,
	"ORDER": true, "BY": true, "GROUP": true, "LIMIT": true, "OFFSET": true,
	"VALUES": true, "INTO": true, "RETURNING": true,
}

// SQL bolds each whole-word keyword in text. Words are maximal runs of
// letters, digits, and underscores; everything else passes through.
func (s Style) SQL(text string) string {
	if !s.on {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		word := text[start:end]
		if sqlKeywords[word] {
			b.WriteString(s.Bold(word))
		} else {
			b.WriteString(word)
		}
		start = -1
	}
	for i, c := range text {
		if isWordByte(c) {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
		b.WriteRune(c)
	}
	flush(len(text))
	return b.String()
}

func isWordByte(c rune) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
