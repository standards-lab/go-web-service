package scenario

import "strings"

// wrap breaks text into lines of at most width columns, breaking only at
// whitespace: a word longer than width stands alone on a line that exceeds
// it rather than being split. A newline in text is a hard break, so a
// paragraph wraps on its own and an empty line between two stays empty.
func wrap(text string, width int) []string {
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		lines = append(lines, wrapParagraph(para, width)...)
	}
	return lines
}

func wrapParagraph(para string, width int) []string {
	words := strings.Fields(para)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	line := words[0]
	for _, word := range words[1:] {
		if len(line)+1+len(word) > width {
			lines = append(lines, line)
			line = word
			continue
		}
		line += " " + word
	}
	return append(lines, line)
}
