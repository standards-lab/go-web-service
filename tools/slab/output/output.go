package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"

	"github.com/standards-lab/go-web-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
)

// Response writes a successful response to w: the body as indented JSON
// when it is non-empty, or, when it is empty or only whitespace, the status
// line ("204 No Content") so the command says something. A non-empty body
// that is not JSON is written as is, since a passthrough command shows what
// the service sent rather than hiding it.
func Response(w io.Writer, status int, body []byte) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		_, _ = fmt.Fprintf(w, "%d %s\n", status, http.StatusText(status))
		return
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err != nil {
		_, _ = fmt.Fprintf(w, "%s\n", body)
		return
	}
	pretty.WriteByte('\n')
	_, _ = w.Write(pretty.Bytes())
}

// ProblemError is a problem document travelling as an error: what Expect
// returns for a response that carries one, and what a command returns for
// a document it decoded itself with httpx.Response.Problem. Error renders
// the document's members when it finds one in the chain, so a command may
// wrap it with context on the way out. The embed's field name is still
// Problem (the unqualified type name), so &ProblemError{Problem: p}
// construction is unchanged; embedding only promotes the document's own
// fields (Status, Title, Detail, ...) onto ProblemError so this file can
// read them directly instead of through a second name.
type ProblemError struct {
	web.Problem
}

// Error is the document on one line: the status and title, and the detail
// when there is one.
func (e *ProblemError) Error() string {
	line := fmt.Sprintf("%d %s", e.Status, e.title())
	if e.Detail != "" {
		line += ": " + e.Detail
	}
	return line
}

// title is the document's title, or the status phrase when the document
// left it out (web.Problem.Write fills the same default server-side).
func (e *ProblemError) title() string {
	if e.Title != "" {
		return e.Title
	}
	return http.StatusText(e.Status)
}

// Expect returns nil when res carries status, and otherwise the error that
// reports the response: a *ProblemError over the RFC 9457 problem document
// the body decodes as, or, when the body is not a problem document,
// httpx.Response.Expect's error naming the status and quoting the body. A
// command checks its one expected status with it and returns what it gets.
func Expect(res *httpx.Response, status int) error {
	if res.Status == status {
		return nil
	}
	if p, err := res.Problem(res.Status); err == nil {
		return &ProblemError{Problem: p}
	}
	return res.Expect(status)
}

// Error writes a failure to w. When err is or wraps a *ProblemError, the
// document's members are written one per line: the status and title first,
// then the detail, the instance, the type (unless it is about:blank, which
// says nothing), and each extension member by name, in key order. Any
// other error is written as its message. A transport error that never
// reached a response, an unexpected non-problem response, and a decoded
// problem document all arrive here through the same parameter.
func Error(w io.Writer, err error) {
	var pe *ProblemError
	if !errors.As(err, &pe) {
		_, _ = fmt.Fprintln(w, err.Error())
		return
	}
	_, _ = fmt.Fprintf(w, "%d %s\n", pe.Status, pe.title())
	if pe.Detail != "" {
		_, _ = fmt.Fprintf(w, "detail: %s\n", pe.Detail)
	}
	if pe.Instance != "" {
		_, _ = fmt.Fprintf(w, "instance: %s\n", pe.Instance)
	}
	if pe.Type != "" && pe.Type != web.ProblemTypeBlank {
		_, _ = fmt.Fprintf(w, "type: %s\n", pe.Type)
	}
	for _, k := range slices.Sorted(maps.Keys(pe.Extras)) {
		_, _ = fmt.Fprintf(w, "%s: %s\n", k, member(pe.Extras[k]))
	}
}

// member renders an extension member's value: a string as is, anything
// else as compact JSON, which is how the document carried it.
func member(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
