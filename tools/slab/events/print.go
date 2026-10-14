package events

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"
	"unicode/utf8"

	"github.com/standards-lab/go-core/event"

	"github.com/standards-lab/go-web-service/tools/slab/output"
	"github.com/standards-lab/go-web-service/tools/slab/style"
)

// Render returns the Render of e as one block under st, then a blank line
// to part it from the next: the type as a status line, as a direct command's response
// shows its status; the id, the subject, and the time, as a table; and the
// data. Data that is JSON prints indented and colored, as a response body
// does; other UTF-8 text prints as it came; anything else prints as its
// size and media type, since a binary payload is never written to a
// terminal. An absent attribute and absent data print nothing.
func Render(st style.Style, e event.Event) output.Render {
	return func(w io.Writer) error { return render(w, st, e) }
}

// render writes e to w, as [Render] describes.
func render(w io.Writer, st style.Style, e event.Event) error {
	if _, err := fmt.Fprintln(w, st.Status(e.Type)); err != nil {
		return err
	}
	rows := [][2]string{{"id", e.ID}}
	if e.Subject != "" {
		rows = append(rows, [2]string{"subject", e.Subject})
	}
	if !e.Time.IsZero() {
		rows = append(rows, [2]string{"time", e.Time.Format(time.RFC3339Nano)})
	}
	if err := output.Table(st, rows)(w); err != nil {
		return err
	}
	if err := printData(w, st, e); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w)
	return err
}

// printData writes e's data, as [Render] describes.
func printData(w io.Writer, st style.Style, e event.Event) error {
	data := bytes.TrimSpace(e.Data)
	if len(data) == 0 {
		return nil
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, data, "", "  "); err == nil {
		_, err := fmt.Fprintln(w, st.JSON(pretty.String()))
		return err
	}
	if utf8.Valid(data) {
		_, err := fmt.Fprintf(w, "%s\n", data)
		return err
	}
	media := e.DataContentType
	if media == "" {
		media = "no media type"
	}
	_, err := fmt.Fprintf(w, "%d bytes of data (%s)\n", len(e.Data), media)
	return err
}
