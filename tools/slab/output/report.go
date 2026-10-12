package output

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/standards-lab/go-web-sdk"
)

// Report is the error a direct command returns for a problem response: the
// decoded document, rendered whole. Its Error is the report, one member per
// line: the status and title first, then the detail, the instance, the type
// (unless it is about:blank, which says nothing), and each extension member
// by name, in key order. go-cli-sdk's dispatcher prints a command's error
// once, after "slab <path>: ", so a command on it returns the Report and the
// whole document reaches stderr; web.Problem's own Error, one line of status
// and detail, would drop the rest. Report unwraps to the web.Problem, so
// errors.As finds the document through it.
type Report struct {
	Problem web.Problem
}

// Error returns the report, its lines joined by newlines with no newline
// after the last, since the dispatcher ends the line itself.
func (r Report) Error() string {
	p := r.Problem
	title := p.Title
	if title == "" {
		title = http.StatusText(p.Status)
	}
	lines := []string{fmt.Sprintf("%d %s", p.Status, title)}
	if p.Detail != "" {
		lines = append(lines, "detail: "+p.Detail)
	}
	if p.Instance != "" {
		lines = append(lines, "instance: "+p.Instance)
	}
	if p.Type != "" && p.Type != web.ProblemTypeBlank {
		lines = append(lines, "type: "+p.Type)
	}
	for _, k := range slices.Sorted(maps.Keys(p.Extras)) {
		lines = append(lines, k+": "+member(p.Extras[k]))
	}
	return strings.Join(lines, "\n")
}

// Unwrap returns the problem document, so errors.As finds a web.Problem
// through a Report.
func (r Report) Unwrap() error {
	return r.Problem
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
