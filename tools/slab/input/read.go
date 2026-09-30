package input

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// DefaultPageSize is the page size a collection read gets when it names
// none: internal/config/reads.go's defaultReadsDefaultSize, which
// config.json leaves in force.
const DefaultPageSize = 20

// ReadFlags is the read grammar every collection read takes: page or
// cursor, size, and sort as their own flags, and each --filter a
// name=value pair passed through as written.
type ReadFlags struct {
	page, size   int
	sort, cursor string
	filters      []string
}

// Bind registers the read flags on cmd, --page and --cursor mutually
// exclusive.
func (r *ReadFlags) Bind(cmd *cobra.Command) {
	cmd.Flags().IntVar(&r.page, "page", 0, "the 1-based page to return; the first when unset")
	cmd.Flags().StringVar(&r.cursor, "cursor", "", "continue after the page whose next token this is, in place of --page")
	cmd.Flags().IntVar(&r.size, "size", 0, fmt.Sprintf("the page size, up to the configured maximum; the service's default (%d) when unset", DefaultPageSize))
	cmd.Flags().StringVar(&r.sort, "sort", "", "comma-separated field names, each with a leading - for descending")
	cmd.Flags().StringArrayVar(&r.filters, "filter", nil, "a filter, field=value or field[op]=value; repeatable")
	cmd.MarkFlagsMutuallyExclusive("page", "cursor")
}

// Query is the read's query pairs from the flags cmd was given, in a fixed
// order (page, cursor, size, sort, then each filter as given): an unset
// flag sends nothing, and a filter without a name is refused before the
// request fires.
func (r *ReadFlags) Query(cmd *cobra.Command) ([][2]string, error) {
	var query [][2]string
	if cmd.Flags().Changed("page") {
		query = append(query, [2]string{"page", strconv.Itoa(r.page)})
	}
	if cmd.Flags().Changed("cursor") {
		query = append(query, [2]string{"cursor", r.cursor})
	}
	if cmd.Flags().Changed("size") {
		query = append(query, [2]string{"size", strconv.Itoa(r.size)})
	}
	if cmd.Flags().Changed("sort") {
		query = append(query, [2]string{"sort", r.sort})
	}
	for _, f := range r.filters {
		name, value, _ := strings.Cut(f, "=")
		if name == "" {
			return nil, fmt.Errorf("--filter %q: want field=value, or field[op]=value", f)
		}
		query = append(query, [2]string{name, value})
	}
	return query, nil
}
