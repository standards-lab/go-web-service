package httpx

import (
	"fmt"
	"net/url"
	"strings"
)

// IfMatch is the precondition header a command carries: the version the
// caller last saw, quoted as an entity tag.
func IfMatch(version int64) Header {
	return Header{Name: "If-Match", Value: fmt.Sprintf(`"%d"`, version)}
}

// RawQuery joins name=value pairs into a query string with each value
// percent-encoded and each name left as written. url.Values.Encode would
// escape the brackets of a name like code[like] to %5B and %5D, and the
// server reads them either way; the literal form is what the printed
// request should show.
func RawQuery(pairs ...[2]string) string {
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = p[0] + "=" + url.QueryEscape(p[1])
	}
	return strings.Join(parts, "&")
}
