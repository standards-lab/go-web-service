package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// Render writes one block of a scenario's narration, the form go-cli-sdk's
// scenario.Reporter.Show takes: Show indents what it writes past the prose
// and prints nothing when it fails. A block carries no caption; a step
// prints the caption as a plain Note before the block, so only the block
// itself is ever colored. Each constructor below takes the style the run
// decided on, so the block is colored only when the run's color is on.
type Render = func(io.Writer) error

// SQL returns the Render of an SQL statement as authored, colored by
// st.SQL (comments dimmed, sqlate markers in the Value color, keywords
// bold), with any trailing newlines dropped so the block ends at its last
// line.
func SQL(st style.Style, text string) Render {
	return func(w io.Writer) error {
		_, err := fmt.Fprintln(w, st.SQL(strings.TrimRight(text, "\n")))
		return err
	}
}

// Table returns the Render of name/value pairs, one per line, the names
// keyed under st and padded so the values line up two spaces past the
// longest name.
func Table(st style.Style, rows [][2]string) Render {
	return aligned(st, rows, "  ")
}

// aligned returns the Render of name/value pairs, one per line: each name
// keyed under st and padded to the longest, then sep, then the value.
func aligned(st style.Style, rows [][2]string, sep string) Render {
	return func(w io.Writer) error {
		width := 0
		for _, row := range rows {
			width = max(width, len(row[0]))
		}
		for _, row := range rows {
			pad := strings.Repeat(" ", width-len(row[0]))
			if _, err := fmt.Fprintf(w, "%s%s%s%s\n", st.Key(row[0]), pad, sep, row[1]); err != nil {
				return err
			}
		}
		return nil
	}
}

// JSON returns the Render of a JSON document as authored, colored under
// st and never reformatted: unlike a wire body, whose layout carries no
// meaning, a file's layout is its author's own. Surrounding whitespace is
// dropped so the block starts and ends at the document.
func JSON(st style.Style, raw []byte) Render {
	return func(w io.Writer) error {
		_, err := fmt.Fprintln(w, st.JSON(string(bytes.TrimSpace(raw))))
		return err
	}
}

// requestContentType is the header httpx.Client.Do sets on its own
// whenever a request carries a body; Request shows it so the block shows
// what is sent.
const requestContentType = "Content-Type"

// Request returns the Render of the outgoing side of one HTTP call as
// httpx.Client.Do sends it: the method and path as its status line, the
// headers, and, after a blank line, the body. Do adds Content-Type:
// application/json whenever body is not nil, so Request shows that header
// too, ahead of the caller's own, unless the caller sets its own
// Content-Type. A []byte or string body shows as it is, indented and
// colored when it is JSON; any other body shows as the JSON Do encodes it
// to, indented and colored. A body that does not encode shows the reason
// in its place, as Do would fail to send it.
func Request(st style.Style, method, path string, headers []httpx.Header, body any) Render {
	return func(w io.Writer) error {
		var b bytes.Buffer
		fmt.Fprintln(&b, st.Status(method+" "+path))
		if body != nil && !hasHeader(headers, requestContentType) {
			writeHeader(&b, st, requestContentType, "application/json")
		}
		for _, h := range headers {
			writeHeader(&b, st, h.Name, h.Value)
		}
		switch v := body.(type) {
		case nil:
		case []byte:
			writeBody(&b, st, v)
		case string:
			writeBody(&b, st, []byte(v))
		default:
			raw, err := json.Marshal(v)
			if err != nil {
				raw = []byte(fmt.Sprintf("(body does not encode: %v)", err))
			}
			writeBody(&b, st, raw)
		}
		_, err := w.Write(b.Bytes())
		return err
	}
}

// shownHeaders are the response headers Response shows when present, in
// this order; every other header is omitted. ETag and Last-Modified are a
// stored object's validators, which a download carries and a conditional
// read names back; a JSON response carries neither.
var shownHeaders = []string{"Content-Type", "ETag", "Last-Modified", "Location", "Traceparent", "X-Request-Id"}

// Response returns the Render of the incoming side of one HTTP call: the
// status line, then, after a blank line, the body when there is one,
// indented and colored when it is JSON, and, after another, the headers
// in shownHeaders that res carries.
func Response(st style.Style, res *httpx.Response) Render {
	return func(w io.Writer) error {
		var b bytes.Buffer
		fmt.Fprintln(&b, st.Status(fmt.Sprintf("HTTP %d %s", res.Status, http.StatusText(res.Status))))
		writeBody(&b, st, res.Body)
		shown := false
		for _, name := range shownHeaders {
			v := res.Header.Get(name)
			if v == "" {
				continue
			}
			if !shown {
				fmt.Fprintln(&b)
				shown = true
			}
			writeHeader(&b, st, name, v)
		}
		_, err := w.Write(b.Bytes())
		return err
	}
}

// Trace returns the Render of the pointer a reader follows by hand to one
// request's trace in Grafana: the Explore page under grafanaBase, the
// service to filter by, and the trace id to search for, one per line, the
// names keyed under st and padded so the colons line up. It is
// deliberately not a deep link: an Explore link carries its query as
// percent-encoded JSON, which is longer than the three lines and no easier
// to follow.
func Trace(st style.Style, grafanaBase, serviceName, traceID string) Render {
	return aligned(st, [][2]string{
		{"Grafana", strings.TrimRight(grafanaBase, "/") + "/explore"},
		{"Service Name", serviceName},
		{"Trace ID", traceID},
	}, ": ")
}

// Metrics returns the Render of the pointer a reader follows by hand to a
// metric query in Grafana, as [Trace] is to a trace: the Explore page under
// grafanaBase, the datasource to pick there, and the PromQL to run, one per
// line, the names keyed under st and padded so the colons line up. It is
// not a deep link, for Trace's reason.
func Metrics(st style.Style, grafanaBase, datasource, promql string) Render {
	return aligned(st, [][2]string{
		{"Grafana", strings.TrimRight(grafanaBase, "/") + "/explore"},
		{"Datasource", datasource},
		{"PromQL", promql},
	}, ": ")
}

// writeHeader writes one header line, its name keyed under st.
func writeHeader(b *bytes.Buffer, st style.Style, name, value string) {
	fmt.Fprintf(b, "%s: %s\n", st.Key(name), value)
}

// writeBody writes an HTTP body after a blank line: a JSON body indented
// and colored under st, any other body as it came, and nothing at all for
// an empty one.
func writeBody(b *bytes.Buffer, st style.Style, raw []byte) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return
	}
	fmt.Fprintln(b)
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		fmt.Fprintf(b, "%s\n", raw)
		return
	}
	fmt.Fprintln(b, st.JSON(pretty.String()))
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
