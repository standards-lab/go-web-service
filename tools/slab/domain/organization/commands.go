package organization

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/input"
	"github.com/standards-lab/go-web-service/tools/slab/output"
)

// deps is what every leaf command needs: the client node it sends through
// and the source of the output it renders its reply through. Bundled once
// here so a leaf builder is a method taking neither, rather than a free
// function repeating both.
type deps struct {
	client *graph.Node[*httpx.Client]
	out    output.Source
}

// Commands builds the org command with one subcommand per endpoint, on
// go-cli-sdk. The org command declares with Use the two nodes every leaf
// below it needs, out's configuration node and client, the HTTP client
// bound to the service's base URL; the dispatcher builds them only when a
// leaf runs, and a leaf reads the client through its Invocation then.
//
// A leaf checks its input in Validate, before anything is built, so a
// missing field flag, version, or media type is a usage error. A leaf's
// Run sends one request, checks the status it expects with output.Expect,
// and renders the reply through out; the error it returns, such as the
// output.Report of a problem response, is printed once by the dispatcher.
func Commands(client *graph.Node[*httpx.Client], out output.Source) *cli.Command {
	d := deps{client: client, out: out}
	org := &cli.Command{
		Name:    "org",
		Summary: "Call the organization domain's endpoints",
	}
	return org.Add(
		d.listCommand(),
		d.getCommand(),
		d.getByPathCommand(),
		d.createCommand(),
		d.editCommand(),
		d.transferCommand(),
		d.deleteCommand(),
		d.logoCommands(),
	).Use(out.Node, client)
}

// newClient returns the organization client over the HTTP client the
// dispatcher built for inv.
func (d deps) newClient(inv *cli.Invocation) *Client {
	return NewClient(inv.Get(d.client))
}

// respond finishes a leaf's Run: it returns err, the request's error, when
// there is one, then output.Expect's error when res does not carry status,
// and otherwise renders the reply through Response.
func (d deps) respond(inv *cli.Invocation, res *httpx.Response, err error, status int) error {
	if err != nil {
		return err
	}
	if err := output.Expect(res, status); err != nil {
		return err
	}
	d.out.New(inv).Response(res.Status, res.Body)
	return nil
}

// listCommand is GET Organizations under the read grammar: page or
// cursor, size, and sort as their own flags, and each --filter a
// name=value pair passed through as written, so the operator syntax
// (code[like]=%o%) is the caller's to spell. --cursor is the next token a
// previous page printed, sent with the same sort and filters.
func (d deps) listCommand() *cli.Command {
	var (
		read  input.ReadFlags
		query [][2]string
	)
	cmd := &cli.Command{
		Name:    "list",
		Summary: "List organizations, one page at a time",
		Args:    cli.NoArgs,
		Validate: func(inv *cli.Invocation) (err error) {
			query, err = read.Query(inv)
			return err
		},
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := d.newClient(inv).List(ctx, query...)
			return d.respond(inv, res, err, http.StatusOK)
		},
	}
	read.Bind(cmd)
	return cmd
}

// getCommand is GET Organizations/{id}.
func (d deps) getCommand() *cli.Command {
	return &cli.Command{
		Name:     "get",
		Summary:  "Read one organization by id",
		Synopsis: "<id>",
		Args:     cli.ExactArgs(1),
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := d.newClient(inv).Find(ctx, inv.Args[0])
			return d.respond(inv, res, err, http.StatusOK)
		},
	}
}

// getByPathCommand is GET Organizations/lookup?path={path}.
func (d deps) getByPathCommand() *cli.Command {
	return &cli.Command{
		Name:     "get-by-path",
		Summary:  "Read one organization by its hierarchy path (acme/engineering)",
		Synopsis: "<path>",
		Args:     cli.ExactArgs(1),
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := d.newClient(inv).FindByPath(ctx, inv.Args[0])
			return d.respond(inv, res, err, http.StatusOK)
		},
	}
}

// createCommand is POST Organizations. The body is --body verbatim, or
// CreateOrganization built from --code, --name, and --parent-id (absent or
// empty for a root).
func (d deps) createCommand() *cli.Command {
	var (
		raw, code, name, parentID string
		body                      []byte
	)
	cmd := &cli.Command{
		Name:    "create",
		Summary: "Create an organization",
		Args:    cli.NoArgs,
		Validate: func(inv *cli.Invocation) (err error) {
			body, err = input.Body(inv, raw, func() (any, error) {
				if !inv.Changed("code") || !inv.Changed("name") {
					return nil, errors.New("--code and --name are required unless --body is given")
				}
				return CreateOrganization{ParentID: optionalParent(parentID), Code: code, Name: name}, nil
			})
			return err
		},
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := d.newClient(inv).Create(ctx, body)
			return d.respond(inv, res, err, http.StatusCreated)
		},
	}
	fs := cmd.Flags()
	fs.StringVar(&code, "code", "", "the organization's code: lowercase words joined by single hyphens")
	fs.StringVar(&name, "name", "", "the organization's name")
	fs.StringVar(&parentID, "parent-id", "", "the parent's id; omit or leave empty for a root")
	fs.StringVar(&raw, "body", "", "the request body as JSON, sent verbatim in place of the field flags")
	cmd.Exclusive("body", "code")
	cmd.Exclusive("body", "name")
	cmd.Exclusive("body", "parent-id")
	return cmd
}

// editCommand is PUT Organizations/{id} under If-Match. The body is --body
// verbatim, or EditOrganization built from --code and --name; the version
// is resolved by input.GuardedBody.
func (d deps) editCommand() *cli.Command {
	var (
		raw, code, name  string
		version, ifMatch int64
		body             []byte
	)
	cmd := &cli.Command{
		Name:     "edit",
		Summary:  "Replace an organization's code and name",
		Synopsis: "<id>",
		Args:     cli.ExactArgs(1),
		Validate: func(inv *cli.Invocation) (err error) {
			body, ifMatch, err = input.GuardedBody(inv, raw, version, func() (any, error) {
				if !inv.Changed("code") || !inv.Changed("name") {
					return nil, errors.New("--code and --name are required unless --body is given")
				}
				return EditOrganization{Code: code, Name: name}, nil
			})
			return err
		},
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := d.newClient(inv).Edit(ctx, inv.Args[0], ifMatch, body)
			return d.respond(inv, res, err, http.StatusOK)
		},
	}
	fs := cmd.Flags()
	fs.StringVar(&code, "code", "", "the organization's code: lowercase words joined by single hyphens")
	fs.StringVar(&name, "name", "", "the organization's name")
	fs.Int64Var(&version, "version", 0, "the version the row was last seen at, sent as If-Match")
	fs.StringVar(&raw, "body", "", `the request body as JSON, sent verbatim in place of the field flags; a top-level "version" stands in for --version`)
	cmd.Exclusive("body", "code")
	cmd.Exclusive("body", "name")
	return cmd
}

// transferCommand is POST Organizations/{id}/transfer under If-Match. The
// body is --body verbatim, or TransferOrganization built from --parent-id,
// which must be given: the service requires the parent_id key and reads
// null as a move to the root, so an unset flag is refused here and an empty
// one sends null.
func (d deps) transferCommand() *cli.Command {
	var (
		raw, parentID    string
		version, ifMatch int64
		body             []byte
	)
	cmd := &cli.Command{
		Name:     "transfer",
		Summary:  "Move an organization under another parent, or to the root",
		Synopsis: "<id>",
		Args:     cli.ExactArgs(1),
		Validate: func(inv *cli.Invocation) (err error) {
			body, ifMatch, err = input.GuardedBody(inv, raw, version, func() (any, error) {
				if !inv.Changed("parent-id") {
					return nil, errors.New("--parent-id is required unless --body is given; pass --parent-id= to move to the root")
				}
				return TransferOrganization{ParentID: optionalParent(parentID)}, nil
			})
			return err
		},
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := d.newClient(inv).Transfer(ctx, inv.Args[0], ifMatch, body)
			return d.respond(inv, res, err, http.StatusOK)
		},
	}
	fs := cmd.Flags()
	fs.StringVar(&parentID, "parent-id", "", "the new parent's id; empty moves the organization to the root")
	fs.Int64Var(&version, "version", 0, "the version the row was last seen at, sent as If-Match")
	fs.StringVar(&raw, "body", "", `the request body as JSON, sent verbatim in place of the field flags; a top-level "version" stands in for --version`)
	cmd.Exclusive("body", "parent-id")
	return cmd
}

// deleteCommand is DELETE Organizations/{id} under If-Match. There is no
// body, so --version is simply required.
func (d deps) deleteCommand() *cli.Command {
	var version int64
	cmd := &cli.Command{
		Name:     "delete",
		Summary:  "Delete an organization",
		Synopsis: "<id>",
		Args:     cli.ExactArgs(1),
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := d.newClient(inv).Delete(ctx, inv.Args[0], version)
			return d.respond(inv, res, err, http.StatusNoContent)
		},
	}
	cmd.Flags().Int64Var(&version, "version", 0, "the version the row was last seen at, sent as If-Match")
	cmd.Require("version")
	return cmd
}

// logoCommands builds the logo subcommand over the organization's logo
// sub-resource: put, get, and delete.
func (d deps) logoCommands() *cli.Command {
	logo := &cli.Command{
		Name:    "logo",
		Summary: "Store, read, and remove an organization's logo",
	}
	return logo.Add(d.logoPutCommand(), d.logoGetCommand(), d.logoDeleteCommand())
}

// logoPutCommand is PUT Organizations/{id}/logo with the file's bytes as
// the raw body, its Content-Type taken from the file's extension unless
// --content-type names one. The service decides which types it accepts.
// The media type is resolved in Validate, from the name alone; the file is
// read in Run, so a file that cannot be read is a failure, not a usage
// error.
func (d deps) logoPutCommand() *cli.Command {
	var contentType, resolved string
	cmd := &cli.Command{
		Name:     "put",
		Summary:  "Store a file as the organization's logo, replacing any",
		Synopsis: "<id> <file>",
		Args:     cli.ExactArgs(2),
		Validate: func(inv *cli.Invocation) error {
			if resolved = contentType; resolved == "" {
				if resolved = mime.TypeByExtension(filepath.Ext(inv.Args[1])); resolved == "" {
					return fmt.Errorf("%s: no media type for its extension; name one with --content-type", inv.Args[1])
				}
			}
			return nil
		},
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			body, err := os.ReadFile(inv.Args[1])
			if err != nil {
				return err
			}
			res, err := d.newClient(inv).PutLogo(ctx, inv.Args[0], resolved, body)
			return d.respond(inv, res, err, http.StatusCreated)
		},
	}
	cmd.Flags().StringVar(&contentType, "content-type", "", "the media type sent; the file extension's when unset")
	return cmd
}

// logoGetCommand is GET Organizations/{id}/logo. It prints the status and
// the object headers, and writes the bytes to --out rather than to the
// terminal. --if-none-match sends a revalidation, answered 304 when the
// logo is unchanged.
func (d deps) logoGetCommand() *cli.Command {
	var out, etag string
	cmd := &cli.Command{
		Name:     "get",
		Summary:  "Read the organization's logo: its headers, and its bytes to --out",
		Synopsis: "<id>",
		Args:     cli.ExactArgs(1),
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := d.newClient(inv).Logo(ctx, inv.Args[0], etag)
			if err != nil {
				return err
			}
			if res.Status != http.StatusNotModified {
				if err := output.Expect(res, http.StatusOK); err != nil {
					return err
				}
			}
			saved := ""
			if out != "" && res.Status == http.StatusOK {
				if err := os.WriteFile(out, res.Body, 0o644); err != nil {
					return err
				}
				saved = out
			}
			d.out.New(inv).Object(res.Status, res.Header, len(res.Body), saved)
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "the file the logo's bytes are written to")
	cmd.Flags().StringVar(&etag, "if-none-match", "", "an ETag a previous get printed; 304 when the logo is unchanged")
	return cmd
}

// logoDeleteCommand is DELETE Organizations/{id}/logo.
func (d deps) logoDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:     "delete",
		Summary:  "Remove the organization's logo",
		Synopsis: "<id>",
		Args:     cli.ExactArgs(1),
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := d.newClient(inv).DeleteLogo(ctx, inv.Args[0])
			return d.respond(inv, res, err, http.StatusNoContent)
		},
	}
}

// optionalParent is the parent_id a flag value stands for: null when the
// flag is empty, the value otherwise.
func optionalParent(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}
