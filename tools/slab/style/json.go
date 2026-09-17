package style

import (
	"encoding/json"
	"strings"
)

// JSON colors an already-indented, already-valid JSON document: object keys
// in one color, string and number values in another, punctuation and
// literals unchanged. It walks the text with encoding/json's tokenizer, so
// it never has to lex JSON itself; on any tokenizer error it returns the
// input unchanged.
func (s Style) JSON(text string) string {
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
	b.WriteString(text[pos:])
	return b.String()
}
