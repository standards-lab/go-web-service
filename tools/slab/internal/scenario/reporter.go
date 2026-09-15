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

// shownHeaders are the response headers HTTP prints when present, in this
// order; every other header is omitted.
var shownHeaders = []string{"Content-Type", "Location", "Traceparent", "X-Request-Id"}

const indent = "  "

// Intent prints step i of n's intent sentence as a heading.
func (r *Reporter) Intent(i, n int, intent string) {
	r.blank()
	r.printf("%s %s\n", r.st.dim(fmt.Sprintf("[%d/%d]", i, n)), r.st.heading(intent))
}

// Note prints one line of prose.
func (r *Reporter) Note(format string, args ...any) {
	r.printf(indent+format+"\n", args...)
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

// HTTP prints a captioned response: its status line, the headers in
// shownHeaders that it carries, and its body. A JSON body is indented and
// colored; any other body prints as it came.
func (r *Reporter) HTTP(caption string, res *httpx.Response) {
	r.caption(caption)
	status := fmt.Sprintf("HTTP %d %s", res.Status, http.StatusText(res.Status))
	r.printf("%s%s%s\n", indent, indent, r.st.status(status))
	for _, name := range shownHeaders {
		if v := res.Header.Get(name); v != "" {
			r.printf("%s%s%s: %s\n", indent, indent, r.st.key(name), v)
		}
	}
	body := bytes.TrimSpace(res.Body)
	if len(body) == 0 {
		return
	}
	r.printf("\n")
	var buf bytes.Buffer
	if err := json.Indent(&buf, body, "", "  "); err != nil {
		r.block(string(body))
		return
	}
	r.block(r.st.jsonColor(buf.String()))
}

// Link prints a captioned deep link and the manual route to the same place
// for when the link cannot be followed.
func (r *Reporter) Link(caption, url, fallback string) {
	r.caption(caption)
	r.printf("%s%s%s\n", indent, indent, r.st.link(url))
	if fallback != "" {
		r.printf("%s%s%s\n", indent, indent, r.st.dim("or: "+fallback))
	}
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

// block prints text with every line indented one level past a caption.
func (r *Reporter) block(text string) {
	for _, line := range strings.Split(text, "\n") {
		r.printf("%s%s%s\n", indent, indent, line)
	}
}
