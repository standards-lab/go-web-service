package style

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// JSON colors an already-indented, already-valid JSON document: object keys
// in one color, string and number values in another, punctuation and
// literals unchanged. It walks the text with encoding/json's tokenizer, so
// it never has to lex JSON itself. Text that fails to tokenize anywhere,
// or ends inside an open object or array, is returned unchanged with no
// color at all, never colored up to the break; the tokenizer reports a
// document cut off between tokens as a plain end of input, so an end with
// a container still open counts as broken too. Between tokens it copies
// the whitespace and punctuation as indented, and each token's literal
// comes from the text, so escapes print exactly as the document carried
// them.
func (s Style) JSON(text string) string {
	if !s.on {
		return text
	}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var b strings.Builder
	b.Grow(len(text) + len(text)/4)
	type frame struct {
		object  bool
		wantKey bool
	}
	var stack []frame
	copied := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) && len(stack) == 0 {
			break
		}
		if err != nil {
			return text
		}
		end := int(dec.InputOffset())
		startTok := copied
		for startTok < end && strings.IndexByte(" \t\r\n:,", text[startTok]) >= 0 {
			startTok++
		}
		if startTok >= end {
			return text
		}
		lit := text[startTok:end]
		b.WriteString(text[copied:startTok])
		copied = end

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
				b.WriteString(s.Key(lit))
			} else {
				b.WriteString(s.Value(lit))
			}
		case json.Number:
			b.WriteString(s.Value(lit))
		default:
			b.WriteString(lit)
		}
		if top >= 0 && stack[top].object {
			stack[top].wantKey = !stack[top].wantKey
		}
	}
	b.WriteString(text[copied:])
	return b.String()
}
