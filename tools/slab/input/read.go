package input

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/standards-lab/go-cli-sdk"
)

// DefaultPageSize is the page size a collection read gets when it names
// none: internal/config/reads.go's defaultReadsDefaultSize, which
// config.json leaves in force.
const DefaultPageSize = 20

// The read flags' help text, shared by the go-cli-sdk and cobra bindings.
var (
	pageUsage   = "the 1-based page to return; the first when unset"
	cursorUsage = "continue after the page whose next token this is, in place of --page"
	sizeUsage   = fmt.Sprintf("the page size, up to the configured maximum; the service's default (%d) when unset", DefaultPageSize)
	sortUsage   = "comma-separated field names, each with a leading - for descending"
	filterUsage = "a filter, field=value or field[op]=value; repeatable"
)

// ReadFlags is the read grammar every collection read takes: page or
// cursor, size, and sort as their own flags, and each --filter a
// name=value pair passed through as written.
type ReadFlags struct {
	page, size   int
	sort, cursor string
	filters      []string
}

// Bind defines the read flags on cmd, --page and --cursor an exclusive
// group, so giving both is a usage error before anything is built.
func (r *ReadFlags) Bind(cmd *cli.Command) {
	fs := cmd.Flags()
	fs.IntVar(&r.page, "page", 0, pageUsage)
	fs.StringVar(&r.cursor, "cursor", "", cursorUsage)
	fs.IntVar(&r.size, "size", 0, sizeUsage)
	fs.StringVar(&r.sort, "sort", "", sortUsage)
	cli.StringsVar(fs, &r.filters, "filter", nil, filterUsage)
	cmd.Exclusive("page", "cursor")
}

// BindCobra is [ReadFlags.Bind] for a command still on cobra, --page and
// --cursor mutually exclusive. It goes when the last such family moves
// onto go-cli-sdk.
func (r *ReadFlags) BindCobra(cmd *cobra.Command) {
	cmd.Flags().IntVar(&r.page, "page", 0, pageUsage)
	cmd.Flags().StringVar(&r.cursor, "cursor", "", cursorUsage)
	cmd.Flags().IntVar(&r.size, "size", 0, sizeUsage)
	cmd.Flags().StringVar(&r.sort, "sort", "", sortUsage)
	cmd.Flags().StringArrayVar(&r.filters, "filter", nil, filterUsage)
	cmd.MarkFlagsMutuallyExclusive("page", "cursor")
}

// Query is the read's query pairs from the flags inv was given, in a fixed
// order (page, cursor, size, sort, then each filter as given): an unset
// flag sends nothing, and a filter without a name is refused before the
// request fires. A command calls it from its Validate, so the refusal is a
// usage error and nothing is built.
func (r *ReadFlags) Query(inv *cli.Invocation) ([][2]string, error) {
	return r.query(inv.Changed)
}

// QueryCobra is [ReadFlags.Query] for a command still on cobra, which
// reads whether each flag was set from cmd's flags. It goes when the last
// such family moves onto go-cli-sdk.
func (r *ReadFlags) QueryCobra(cmd *cobra.Command) ([][2]string, error) {
	return r.query(cmd.Flags().Changed)
}

// query is [ReadFlags.Query] over changed, which reports whether a flag
// was set.
func (r *ReadFlags) query(changed func(name string) bool) ([][2]string, error) {
	var query [][2]string
	if changed("page") {
		query = append(query, [2]string{"page", strconv.Itoa(r.page)})
	}
	if changed("cursor") {
		query = append(query, [2]string{"cursor", r.cursor})
	}
	if changed("size") {
		query = append(query, [2]string{"size", strconv.Itoa(r.size)})
	}
	if changed("sort") {
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
