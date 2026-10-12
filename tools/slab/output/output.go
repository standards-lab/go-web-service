package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// Output is where a direct command's result goes and how it looks: the
// stream a success is written to, and whether it is colored. The
// composition root constructs one and every command family renders through
// it, so no command names a stream or a style itself. A failure is not
// written here: a command returns it, and the dispatcher reports it.
//
// Output holds the color decision as a function and asks it each time it
// renders, which is always from a command's run, after parsing. Nothing
// about an Output changes after New, so one is safe to share.
type Output struct {
	stdout io.Writer
	color  func() bool
}

// New returns an Output writing results to stdout. color reports, when
// asked, whether to emit ANSI color; it is asked at each render, never at
// New. A nil color never colors.
func New(stdout io.Writer, color func() bool) *Output {
	if color == nil {
		color = func() bool { return false }
	}
	return &Output{stdout: stdout, color: color}
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
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		o.Status(status)
		return
	}
	st := o.Style()
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err != nil {
		_, _ = fmt.Fprintf(o.stdout, "%s\n", body)
		return
	}
	_, _ = fmt.Fprintf(o.stdout, "%s\n", st.JSON(pretty.String()))
}

// Status writes a bodiless response to stdout: the status line, styled as
// Response styles it, then each of lines as written. A command whose reply
// is a status and a header or two, such as a 202 and its Location, says
// what happened through it rather than printing the status line alone.
func (o *Output) Status(status int, lines ...string) {
	st := o.Style()
	_, _ = fmt.Fprintf(o.stdout, "%s\n", st.Status(fmt.Sprintf("%d %s", status, http.StatusText(status))))
	for _, line := range lines {
		_, _ = fmt.Fprintln(o.stdout, line)
	}
}

// objectHeaders are the headers an object response carries that Object
// shows, in the order it shows them. Content-Disposition is the name a
// document download is served under.
var objectHeaders = []string{"Content-Type", "Content-Length", "Content-Disposition", "ETag", "Last-Modified", "Cache-Control", "X-Content-Type-Options"}

// Object writes a proxied object's response to stdout: the status line, the
// object headers it carries, and what became of the bytes, since a binary
// body is never written to a terminal. saved is the file the bytes went to,
// or empty when they were only counted.
func (o *Output) Object(status int, header http.Header, n int, saved string) {
	st := o.Style()
	_, _ = fmt.Fprintf(o.stdout, "%s\n", st.Status(fmt.Sprintf("%d %s", status, http.StatusText(status))))
	for _, name := range objectHeaders {
		if v := header.Get(name); v != "" {
			_, _ = fmt.Fprintf(o.stdout, "%s: %s\n", name, v)
		}
	}
	switch {
	case saved != "":
		_, _ = fmt.Fprintf(o.stdout, "%d bytes written to %s\n", n, saved)
	case n > 0:
		_, _ = fmt.Fprintf(o.stdout, "%d bytes (--out saves them)\n", n)
	}
}

// Expect returns nil when res carries status, and otherwise the error that
// reports the response: a [Report] of the decoded web.Problem when the body
// is an RFC 9457 document, or, when the body is not a problem document,
// httpx.Response.Expect's error naming the status and quoting the body. A
// command checks its one expected status with it and returns what it gets.
func Expect(res *httpx.Response, status int) error {
	if res.Status == status {
		return nil
	}
	if p, err := res.Problem(res.Status); err == nil {
		return Report{Problem: p}
	}
	return res.Expect(status)
}
