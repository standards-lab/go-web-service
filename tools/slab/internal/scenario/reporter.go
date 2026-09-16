package scenario

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/standards-lab/go-web-service/tools/slab/internal/httpx"
)

// Reporter is the observation channels every scenario narrates through. Each
// method writes one block to the reporter's writer, colored when the
// reporter was built with color on.
type Reporter struct {
	w       io.Writer
	st      style
	atBlank bool // whether the line just written was blank
}

// NewReporter returns a Reporter writing to w, with ANSI color on or off.
func NewReporter(w io.Writer, color bool) *Reporter {
	return &Reporter{w: w, st: style{on: color}}
}

// shownHeaders are the response headers Response prints when present, in
// this order; every other header is omitted.
var shownHeaders = []string{"Content-Type", "Location", "Traceparent", "X-Request-Id"}

// contentType is the header httpx.Client.Do sets on its own whenever a
// request carries a body; Request prints it so the block shows what is sent.
const contentType = "Content-Type"

const indent = "  "

// columns is the width Note wraps prose to, the indent included.
const columns = 80

// Intent prints step i of n's intent sentence as a heading.
func (r *Reporter) Intent(i, n int, intent string) {
	r.blank()
	r.printf("%s %s\n", r.st.dim(fmt.Sprintf("[%d/%d]", i, n)), r.st.heading(intent))
}

// Note prints prose, wrapped at columns with every line indented: one call
// is one description, however many lines it takes.
func (r *Reporter) Note(format string, args ...any) {
	for _, line := range wrap(fmt.Sprintf(format, args...), columns-len(indent)) {
		if line == "" {
			r.blank()
			continue
		}
		r.printf("%s%s\n", indent, line)
	}
}

// SQL prints a captioned SQL block with its keywords in bold, set off by a
// blank line above and below so the block reads apart from the narration
// around it.
func (r *Reporter) SQL(caption, text string) {
	r.blank()
	r.caption(caption)
	r.block(r.st.sql(strings.TrimRight(text, "\n")))
	r.blank()
}

// JSON prints a captioned JSON document the way SQL prints a statement: as
// authored, colored, never reformatted — unlike a wire body, whose layout
// carries no meaning, a file's layout is the author's own and stays as
// written.
func (r *Reporter) JSON(caption string, raw []byte) {
	r.blank()
	r.caption(caption)
	r.block(r.st.jsonColor(string(bytes.TrimSpace(raw))))
	r.blank()
}

// Table prints captioned name/value pairs with the names aligned.
func (r *Reporter) Table(caption string, rows [][2]string) {
	r.caption(caption)
	width := 0
	for _, row := range rows {
		width = max(width, len(row[0]))
	}
	for _, row := range rows {
		pad := strings.Repeat(" ", width-len(row[0]))
		r.printf("%s%s%s%s  %s\n", indent, indent, r.st.key(row[0]), pad, row[1])
	}
}

// Request prints the outgoing side of one HTTP call as httpx.Client.Do sends
// it: the method and path, the headers, and the body. Do adds Content-Type:
// application/json whenever body is not nil, so Request prints that header
// too, ahead of the caller's own unless the caller set its own Content-Type.
// A []byte or string body prints as it is; any other body prints as the JSON
// Do encodes it to, indented and colored.
func (r *Reporter) Request(method, path string, headers []httpx.Header, body any) {
	r.blank()
	r.printf("%s%s\n", indent, r.st.status(method+" "+path))
	if body != nil && !hasHeader(headers, contentType) {
		r.header(contentType, "application/json")
	}
	for _, h := range headers {
		r.header(h.Name, h.Value)
	}
	switch b := body.(type) {
	case nil:
	case []byte:
		r.body(b)
	case string:
		r.body([]byte(b))
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			r.body([]byte(fmt.Sprintf("(body does not encode: %v)", err)))
			break
		}
		r.body(raw)
	}
	r.blank()
}

// Response prints the incoming side: the status line, the body when there
// is one, and the headers in shownHeaders that the response carries.
func (r *Reporter) Response(res *httpx.Response) {
	r.blank()
	status := fmt.Sprintf("HTTP %d %s", res.Status, http.StatusText(res.Status))
	r.printf("%s%s\n", indent, r.st.status(status))
	r.body(res.Body)
	shown := false
	for _, name := range shownHeaders {
		v := res.Header.Get(name)
		if v == "" {
			continue
		}
		if !shown {
			r.blank()
			shown = true
		}
		r.header(name, v)
	}
	r.blank()
}

// Trace prints the pointer a viewer follows by hand to one request's trace
// in Grafana: the Explore page under grafanaBase, the service to filter by,
// and the trace id to search for. It is deliberately not a deep link; an
// Explore link carries its query as percent-encoded JSON, which is longer
// than the three lines and no easier to follow.
func (r *Reporter) Trace(grafanaBase, serviceName, traceID string) {
	r.blank()
	r.printf("%s%s\n", indent, r.st.status("Observability"))
	rows := [][2]string{
		{"Grafana", strings.TrimRight(grafanaBase, "/") + "/explore"},
		{"Service Name", serviceName},
		{"Trace ID", traceID},
	}
	width := 0
	for _, row := range rows {
		width = max(width, len(row[0]))
	}
	for _, row := range rows {
		pad := strings.Repeat(" ", width-len(row[0]))
		r.printf("%s%s%s%s: %s\n", indent, indent, r.st.key(row[0]), pad, row[1])
	}
	r.blank()
}

// Tick prints one line from a repeating action.
func (r *Reporter) Tick(format string, args ...any) {
	r.printf(indent+indent+r.st.dim("·")+" "+format+"\n", args...)
}

// printf is the one write every channel goes through; a reporter has no way
// to act on a failed write, so the result is discarded here. Only blank's own
// call ever passes the bare "\n" format, so that is what atBlank tracks.
func (r *Reporter) printf(format string, args ...any) {
	_, _ = fmt.Fprintf(r.w, format, args...)
	r.atBlank = format == "\n"
}

// blank prints one blank line, unless the reporter already sits on one: two
// blocks back to back (SQL, in particular) each open and close with a blank
// line, and without this guard a pair of them would print two.
func (r *Reporter) blank() {
	if r.atBlank {
		return
	}
	r.printf("\n")
}

func (r *Reporter) caption(text string) {
	r.printf("%s%s\n", indent, r.st.caption(text))
}

// header prints one header line at block depth.
func (r *Reporter) header(name, value string) {
	r.printf("%s%s%s: %s\n", indent, indent, r.st.key(name), value)
}

// body prints an HTTP body at block depth, set off by a blank line: a JSON
// body indented and colored, any other body as it came, and nothing at all
// for an empty one.
func (r *Reporter) body(raw []byte) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return
	}
	r.blank()
	r.block(r.jsonText(raw))
}

// jsonText returns raw indented and colored when it is a JSON document, and as
// it came when it is not.
func (r *Reporter) jsonText(raw []byte) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw)
	}
	return r.st.jsonColor(buf.String())
}

// hasHeader reports whether headers names name, compared as HTTP does.
func hasHeader(headers []httpx.Header, name string) bool {
	for _, h := range headers {
		if strings.EqualFold(h.Name, name) {
			return true
		}
	}
	return false
}

// block prints text with every line indented one level past a caption.
func (r *Reporter) block(text string) {
	for _, line := range strings.Split(text, "\n") {
		r.printf("%s%s%s\n", indent, indent, line)
	}
}
