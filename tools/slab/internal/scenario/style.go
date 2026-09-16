package scenario

import (
	"encoding/json"
	"os"
	"strings"

	"golang.org/x/term"
)

// ColorEnabled reports whether the reporter should emit ANSI color: only when
// stdout is a terminal, NO_COLOR is unset, and the --no-color flag was not
// passed.
func ColorEnabled(noColor bool) bool {
	return !noColor && os.Getenv("NO_COLOR") == "" && term.IsTerminal(int(os.Stdout.Fd()))
}

// style is the one place the reporter's ANSI escapes live. With on false
// every method returns its input unchanged, so the same code path serves a
// pipe and a terminal.
type style struct{ on bool }

const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiBlue    = "\x1b[34m"
	ansiMagenta = "\x1b[35m"
	ansiCyan    = "\x1b[36m"
)

func (s style) wrap(code, text string) string {
	if !s.on || text == "" {
		return text
	}
	return code + text + ansiReset
}

func (s style) bold(text string) string    { return s.wrap(ansiBold, text) }
func (s style) dim(text string) string     { return s.wrap(ansiDim, text) }
func (s style) heading(text string) string { return s.wrap(ansiBold+ansiCyan, text) }
func (s style) caption(text string) string { return s.wrap(ansiMagenta, text) }
func (s style) key(text string) string     { return s.wrap(ansiBlue, text) }
func (s style) value(text string) string   { return s.wrap(ansiGreen, text) }
func (s style) status(text string) string  { return s.wrap(ansiBold+ansiYellow, text) }

// sqlKeywords is the hand-maintained list SQL bolds. Match is exact: the
// repository's statements write keywords in upper case.
var sqlKeywords = map[string]bool{
	"SELECT": true, "FROM": true, "WHERE": true, "INSERT": true, "UPDATE": true,
	"DELETE": true, "SET": true, "JOIN": true, "ON": true, "AND": true, "OR": true,
	"ORDER": true, "BY": true, "GROUP": true, "LIMIT": true, "OFFSET": true,
	"VALUES": true, "INTO": true, "RETURNING": true,
}

// sql bolds each whole-word keyword in text. Words are maximal runs of
// letters, digits, and underscores; everything else passes through.
func (s style) sql(text string) string {
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
			b.WriteString(s.bold(word))
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

// jsonColor colors an already-indented, already-valid JSON document: object
// keys in one color, string and number values in another, punctuation and
// literals unchanged. It walks the text with encoding/json's tokenizer, so
// it never has to lex JSON itself; on any tokenizer error it returns the
// input unchanged.
func (s style) jsonColor(text string) string {
	if !s.on {
		return text
	}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var b strings.Builder
	b.Grow(len(text) + len(text)/4)
	// stack records, per open container, whether the next scalar is an
	// object key (objects alternate key, value; arrays hold only values).
	type frame struct {
		object  bool
		wantKey bool
	}
	var stack []frame
	pos := 0 // bytes of text copied so far
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		// Everything between the last token's end and this token's start
		// is whitespace and punctuation Indent put there; copy it as is
		// and take the token's literal from the source text, so escapes
		// print exactly as the body carried them.
		end := int(dec.InputOffset())
		startTok := pos
		for startTok < end && strings.IndexByte(" \t\r\n:,", text[startTok]) >= 0 {
			startTok++
		}
		if startTok >= end {
			return text
		}
		lit := text[startTok:end]
		b.WriteString(text[pos:startTok])
		pos = end

		top := len(stack) - 1
		isKey := top >= 0 && stack[top].object && stack[top].wantKey
		switch v := tok.(type) {
		case json.Delim:
			b.WriteString(lit)
			switch v {
			case '{':
				stack = append(stack, frame{object: true, wantKey: true})
			case '[':
				stack = append(stack, frame{})
			case '}', ']':
				if top >= 0 {
					stack = stack[:top]
				}
				if t := len(stack) - 1; t >= 0 && stack[t].object {
					stack[t].wantKey = true
				}
			}
			continue
		case string:
			if isKey {
				b.WriteString(s.key(lit))
			} else {
				b.WriteString(s.value(lit))
			}
		case json.Number:
			b.WriteString(s.value(lit))
		default:
			b.WriteString(lit)
		}
		if top >= 0 && stack[top].object {
			stack[top].wantKey = !stack[top].wantKey
		}
	}
	b.WriteString(text[pos:])
	return b.String()
}
