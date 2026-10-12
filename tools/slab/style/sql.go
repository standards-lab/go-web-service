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

const (
	sqlComment     = "--"
	sqlMarkerOpen  = "{{"
	sqlMarkerClose = "}}"
)

// SQL colors text line by line. A line whose first non-space characters
// are -- (a sqlate --| annotation or a plain comment) is dimmed whole, and
// nothing inside it is colored otherwise. On any other line, a sqlate
// marker ({{code}}, {{parent_id:uuid}}, {{> app.identity}}) takes the
// Value color, braces included; a -- outside a marker dims the rest of the
// line as a trailing comment; and in the code between, each whole-word
// keyword is bold. Words are maximal runs of letters, digits, and
// underscores. A -- inside a string literal is taken for a comment too:
// the repository's statements bind values rather than quoting them.
func (s Style) SQL(text string) string {
	if !s.on {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		s.sqlLine(&b, line)
	}
	return b.String()
}

// sqlLine writes one line of SQL's coloring to b.
func (s Style) sqlLine(b *strings.Builder, line string) {
	if strings.HasPrefix(strings.TrimLeft(line, " \t"), sqlComment) {
		b.WriteString(s.Dim(line))
		return
	}
	for line != "" {
		comment := strings.Index(line, sqlComment)
		open := strings.Index(line, sqlMarkerOpen)
		if comment >= 0 && (open < 0 || comment < open) {
			s.sqlCode(b, line[:comment])
			b.WriteString(s.Dim(line[comment:]))
			return
		}
		if open < 0 {
			s.sqlCode(b, line)
			return
		}
		end := strings.Index(line[open:], sqlMarkerClose)
		if end < 0 {
			// An unclosed {{ is no marker: the line is code to its end.
			s.sqlCode(b, line)
			return
		}
		end += open + len(sqlMarkerClose)
		s.sqlCode(b, line[:open])
		b.WriteString(s.Value(line[open:end]))
		line = line[end:]
	}
}

// sqlCode writes code, which holds no comment or marker, to b with each
// whole-word keyword bold.
func (s Style) sqlCode(b *strings.Builder, code string) {
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		word := code[start:end]
		if sqlKeywords[word] {
			b.WriteString(s.Bold(word))
		} else {
			b.WriteString(word)
		}
		start = -1
	}
	for i, c := range code {
		if isWordByte(c) {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
		b.WriteRune(c)
	}
	flush(len(code))
}

func isWordByte(c rune) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
