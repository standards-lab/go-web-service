package organization

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/input"
	"github.com/standards-lab/go-web-service/tools/slab/output"
)

// deps is what every leaf command needs: the client constructor and the
// output to render its reply through. Bundled once here so a leaf builder
// is a method taking neither, rather than a free function repeating both.
type deps struct {
	newClient func() *Client
	out       *output.Output
}

// Commands builds the org command with one subcommand per endpoint. Each
// subcommand's RunE calls newClient when it runs, not when the tree is
// built: the composition root closes newClient over its persistent flags,
// which cobra parses during execution, so the base URL is unknown until
// then. out is the output every subcommand renders its reply through,
// constructed by the same root. A subcommand returns its error unrendered;
// the root writes it through out's Error and sets the exit code.
func Commands(newClient func() *Client, out *output.Output) *cobra.Command {
	d := deps{newClient: newClient, out: out}
	org := &cobra.Command{
		Use:   "org",
		Short: "Call the organization domain's endpoints",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	org.AddCommand(
		d.listCommand(),
		d.getCommand(),
		d.getByPathCommand(),
		d.createCommand(),
		d.editCommand(),
		d.transferCommand(),
		d.deleteCommand(),
	)
	return org
}

// listCommand is GET Organizations under the read grammar: page, size, and
// sort as their own flags, and each --filter a name=value pair passed
// through as written, so the operator syntax (code[like]=%o%) is the
// caller's to spell.
func (d deps) listCommand() *cobra.Command {
	var (
		page, size int
		sort       string
		filters    []string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List organizations, one page at a time",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var query [][2]string
			if cmd.Flags().Changed("page") {
				query = append(query, [2]string{"page", strconv.Itoa(page)})
			}
			if cmd.Flags().Changed("size") {
				query = append(query, [2]string{"size", strconv.Itoa(size)})
			}
			if cmd.Flags().Changed("sort") {
				query = append(query, [2]string{"sort", sort})
			}
			for _, f := range filters {
				name, value, _ := strings.Cut(f, "=")
				if name == "" {
					return fmt.Errorf("--filter %q: want field=value, or field[op]=value", f)
				}
				query = append(query, [2]string{name, value})
			}
			res, err := d.newClient().List(cmd.Context(), query...)
			if err != nil {
				return err
			}
			if err := output.Expect(res, http.StatusOK); err != nil {
				return err
			}
			d.out.Response(res.Status, res.Body)
			return nil
		},
	}
	cmd.Flags().IntVar(&page, "page", 0, "the 1-based page to return; the first when unset")
	cmd.Flags().IntVar(&size, "size", 0, fmt.Sprintf("the page size, up to the configured maximum; the service's default (%d) when unset", DefaultPageSize))
	cmd.Flags().StringVar(&sort, "sort", "", "comma-separated field names, each with a leading - for descending")
	cmd.Flags().StringArrayVar(&filters, "filter", nil, "a filter, field=value or field[op]=value; repeatable")
	return cmd
}

// getCommand is GET Organizations/{id}.
func (d deps) getCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Read one organization by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := d.newClient().Find(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := output.Expect(res, http.StatusOK); err != nil {
				return err
			}
			d.out.Response(res.Status, res.Body)
			return nil
		},
	}
}

// getByPathCommand is GET Organizations/path/{path}.
func (d deps) getByPathCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get-by-path <path>",
		Short: "Read one organization by its hierarchy path (acme/engineering)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := d.newClient().FindByPath(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := output.Expect(res, http.StatusOK); err != nil {
				return err
			}
			d.out.Response(res.Status, res.Body)
			return nil
		},
	}
}

// createCommand is POST Organizations. The body is --body verbatim, or
// CreateOrganization built from --code, --name, and --parent-id (absent or
// empty for a root).
func (d deps) createCommand() *cobra.Command {
	var (
		raw, code, name, parentID string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an organization",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := input.Body(cmd, raw, func() (any, error) {
				if !cmd.Flags().Changed("code") || !cmd.Flags().Changed("name") {
					return nil, errors.New("--code and --name are required unless --body is given")
				}
				return CreateOrganization{ParentID: optionalParent(parentID), Code: code, Name: name}, nil
			})
			if err != nil {
				return err
			}
			res, err := d.newClient().Create(cmd.Context(), body)
			if err != nil {
				return err
			}
			if err := output.Expect(res, http.StatusCreated); err != nil {
				return err
			}
			d.out.Response(res.Status, res.Body)
			return nil
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "the organization's code: lowercase words joined by single hyphens")
	cmd.Flags().StringVar(&name, "name", "", "the organization's name")
	cmd.Flags().StringVar(&parentID, "parent-id", "", "the parent's id; omit or leave empty for a root")
	cmd.Flags().StringVar(&raw, "body", "", "the request body as JSON, sent verbatim in place of the field flags")
	cmd.MarkFlagsMutuallyExclusive("body", "code")
	cmd.MarkFlagsMutuallyExclusive("body", "name")
	cmd.MarkFlagsMutuallyExclusive("body", "parent-id")
	return cmd
}

// editCommand is PUT Organizations/{id} under If-Match. The body is --body
// verbatim, or EditOrganization built from --code and --name; the version
// is resolved by input.GuardedBody.
func (d deps) editCommand() *cobra.Command {
	var (
		raw, code, name string
		version         int64
	)
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Replace an organization's code and name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, ifMatch, err := input.GuardedBody(cmd, raw, version, func() (any, error) {
				if !cmd.Flags().Changed("code") || !cmd.Flags().Changed("name") {
					return nil, errors.New("--code and --name are required unless --body is given")
				}
				return EditOrganization{Code: code, Name: name}, nil
			})
			if err != nil {
				return err
			}
			res, err := d.newClient().Edit(cmd.Context(), args[0], ifMatch, body)
			if err != nil {
				return err
			}
			if err := output.Expect(res, http.StatusOK); err != nil {
				return err
			}
			d.out.Response(res.Status, res.Body)
			return nil
		},
	}
	cmd.Flags().StringVar(&code, "code", "", "the organization's code: lowercase words joined by single hyphens")
	cmd.Flags().StringVar(&name, "name", "", "the organization's name")
	cmd.Flags().Int64Var(&version, "version", 0, "the version the row was last seen at, sent as If-Match")
	cmd.Flags().StringVar(&raw, "body", "", `the request body as JSON, sent verbatim in place of the field flags; a top-level "version" stands in for --version`)
	cmd.MarkFlagsMutuallyExclusive("body", "code")
	cmd.MarkFlagsMutuallyExclusive("body", "name")
	return cmd
}

// transferCommand is POST Organizations/{id}/transfer under If-Match. The
// body is --body verbatim, or TransferOrganization built from --parent-id,
// which must be given: the service requires the parent_id key and reads
// null as a move to the root, so an unset flag is refused here and an empty
// one sends null.
func (d deps) transferCommand() *cobra.Command {
	var (
		raw, parentID string
		version       int64
	)
	cmd := &cobra.Command{
		Use:   "transfer <id>",
		Short: "Move an organization under another parent, or to the root",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, ifMatch, err := input.GuardedBody(cmd, raw, version, func() (any, error) {
				if !cmd.Flags().Changed("parent-id") {
					return nil, errors.New("--parent-id is required unless --body is given; pass --parent-id= to move to the root")
				}
				return TransferOrganization{ParentID: optionalParent(parentID)}, nil
			})
			if err != nil {
				return err
			}
			res, err := d.newClient().Transfer(cmd.Context(), args[0], ifMatch, body)
			if err != nil {
				return err
			}
			if err := output.Expect(res, http.StatusOK); err != nil {
				return err
			}
			d.out.Response(res.Status, res.Body)
			return nil
		},
	}
	cmd.Flags().StringVar(&parentID, "parent-id", "", "the new parent's id; empty moves the organization to the root")
	cmd.Flags().Int64Var(&version, "version", 0, "the version the row was last seen at, sent as If-Match")
	cmd.Flags().StringVar(&raw, "body", "", `the request body as JSON, sent verbatim in place of the field flags; a top-level "version" stands in for --version`)
	cmd.MarkFlagsMutuallyExclusive("body", "parent-id")
	return cmd
}

// deleteCommand is DELETE Organizations/{id} under If-Match. There is no
// body, so --version is simply required.
func (d deps) deleteCommand() *cobra.Command {
	var version int64
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete an organization",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := d.newClient().Delete(cmd.Context(), args[0], version)
			if err != nil {
				return err
			}
			if err := output.Expect(res, http.StatusNoContent); err != nil {
				return err
			}
			d.out.Response(res.Status, res.Body)
			return nil
		},
	}
	cmd.Flags().Int64Var(&version, "version", 0, "the version the row was last seen at, sent as If-Match")
	_ = cmd.MarkFlagRequired("version")
	return cmd
}

// optionalParent is the parent_id a flag value stands for: null when the
// flag is empty, the value otherwise.
func optionalParent(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}
