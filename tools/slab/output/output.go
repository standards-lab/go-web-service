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
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// Output is where a direct command's result goes and how it looks: the
// stream a success is written to, the stream a failure is written to, and
// whether either is colored. The composition root constructs one and every
// command family renders through it, so no command names a stream or a
// style itself.
//
// The streams are known when the tree is built; whether to color is not,
// since --no-color is a persistent flag cobra parses during execution. So
// Output holds the decision as a function and asks it each time it renders,
// which is always from a command's RunE, after parsing. Nothing about an
// Output changes after New, so one is safe to share.
type Output struct {
	stdout, stderr io.Writer
	color          func() bool
}

// New returns an Output writing results to stdout and failures to stderr.
// color reports, when asked, whether to emit ANSI color; it is asked at
// each render, never at New. A nil color never colors.
func New(stdout, stderr io.Writer, color func() bool) *Output {
	if color == nil {
		color = func() bool { return false }
	}
	return &Output{stdout: stdout, stderr: stderr, color: color}
}

// Style is the styling as the run stands now: color on when color says so.
// A command that renders something Response does not can style it the same
// way.
func (o *Output) Style() style.Style {
	return style.New(o.color())
}

// Response writes a successful response to stdout: the body as indented,
// colored JSON when it is non-empty, or, when it is empty or only
// whitespace, the status line ("204 No Content") so the command says
// something. A non-empty body that is not JSON is written as is, since a
// passthrough command shows what the service sent rather than hiding it.
func (o *Output) Response(status int, body []byte) {
	st := o.Style()
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		_, _ = fmt.Fprintf(o.stdout, "%s\n", st.Status(fmt.Sprintf("%d %s", status, http.StatusText(status))))
		return
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err != nil {
		_, _ = fmt.Fprintf(o.stdout, "%s\n", body)
		return
	}
	_, _ = fmt.Fprintf(o.stdout, "%s\n", st.JSON(pretty.String()))
}

// Expect returns nil when res carries status, and otherwise the error that
// reports the response: the decoded web.Problem itself when the body is an
// RFC 9457 document — web.Problem implements error directly, so a command
// returns it with no wrapper needed — or, when the body is not a problem
// document, httpx.Response.Expect's error naming the status and quoting the
// body. A command checks its one expected status with it and returns what
// it gets.
func Expect(res *httpx.Response, status int) error {
	if res.Status == status {
		return nil
	}
	if p, err := res.Problem(res.Status); err == nil {
		return p
	}
	return res.Expect(status)
}

// Error writes a failure to stderr. When err is or wraps a web.Problem, the
// document's members are written one per line: the status and title first,
// then the detail, the instance, the type (unless it is about:blank, which
// says nothing), and each extension member by name, in key order. Any
// other error is written as its message. A transport error that never
// reached a response, an unexpected non-problem response, and a decoded
// problem document all arrive here through the same parameter.
func (o *Output) Error(err error) {
	w := o.stderr
	var p web.Problem
	if !errors.As(err, &p) {
		_, _ = fmt.Fprintln(w, err.Error())
		return
	}
	title := p.Title
	if title == "" {
		title = http.StatusText(p.Status)
	}
	_, _ = fmt.Fprintf(w, "%d %s\n", p.Status, title)
	if p.Detail != "" {
		_, _ = fmt.Fprintf(w, "detail: %s\n", p.Detail)
	}
	if p.Instance != "" {
		_, _ = fmt.Fprintf(w, "instance: %s\n", p.Instance)
	}
	if p.Type != "" && p.Type != web.ProblemTypeBlank {
		_, _ = fmt.Fprintf(w, "type: %s\n", p.Type)
	}
	for _, k := range slices.Sorted(maps.Keys(p.Extras)) {
		_, _ = fmt.Fprintf(w, "%s: %s\n", k, member(p.Extras[k]))
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
