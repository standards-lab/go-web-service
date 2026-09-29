package document

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/input"
	"github.com/standards-lab/go-web-service/tools/slab/output"
)

// octetStream is the media type an upload is sent under when its file's
// extension names none and --content-type is unset. The service accepts any
// type, so an unknown extension is no reason to refuse.
const octetStream = "application/octet-stream"

// deps is what every leaf command needs: the client constructor and the
// output to render its reply through, bundled as on the organization
// commands.
type deps struct {
	newClient func() *Client
	out       *output.Output
}

// Commands builds the docs command with a dirs group and a files group, one
// leaf per endpoint. Each leaf's RunE calls newClient when it runs, not when
// the tree is built, since the base URL is a persistent flag cobra parses
// during execution. out is the output every leaf renders its reply through.
// A leaf returns its error unrendered; the root writes it through out's
// Error and sets the exit code.
func Commands(newClient func() *Client, out *output.Output) *cobra.Command {
	d := deps{newClient: newClient, out: out}
	docs := group("docs", "Call the document domain's endpoints")
	dirs := group("dirs", "Create, read, list, move, and delete an organization's directories")
	dirs.AddCommand(
		d.dirsCreateCommand(),
		d.dirsGetCommand(),
		d.dirsListCommand(),
		d.dirsMoveCommand(),
		d.dirsDeleteCommand(),
	)
	files := group("files", "List, upload, read, move, and delete an organization's files")
	files.AddCommand(
		d.filesListCommand(),
		d.filesPutCommand(),
		d.filesShowCommand(),
		d.filesGetCommand(),
		d.filesMoveCommand(),
		d.filesDeleteCommand(),
	)
	docs.AddCommand(dirs, files)
	return docs
}

// group is a command that only holds subcommands: run alone, it prints its
// help.
func group(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
}

// readFlags is the read grammar both listings take, as org list takes it:
// page or cursor, size, and sort as their own flags, and each --filter a
// name=value pair passed through as written.
type readFlags struct {
	page, size   int
	sort, cursor string
	filters      []string
}

// bind registers the read flags on cmd, --page and --cursor mutually
// exclusive.
func (r *readFlags) bind(cmd *cobra.Command) {
	cmd.Flags().IntVar(&r.page, "page", 0, "the 1-based page to return; the first when unset")
	cmd.Flags().StringVar(&r.cursor, "cursor", "", "continue after the page whose next token this is, in place of --page")
	cmd.Flags().IntVar(&r.size, "size", 0, fmt.Sprintf("the page size, up to the configured maximum; the service's default (%d) when unset", DefaultPageSize))
	cmd.Flags().StringVar(&r.sort, "sort", "", "comma-separated field names, each with a leading - for descending")
	cmd.Flags().StringArrayVar(&r.filters, "filter", nil, "a filter, field=value or field[op]=value; repeatable")
	cmd.MarkFlagsMutuallyExclusive("page", "cursor")
}

// query is the listing's query pairs from the flags cmd was given, in flag
// order: an unset flag sends nothing, and a filter without a name is
// refused before the request fires.
func (r *readFlags) query(cmd *cobra.Command) ([][2]string, error) {
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

// dirsCreateCommand is POST Documents/{org}/directories. The body is --body
// verbatim, or CreateDirectory built from --parent-id and --name.
func (d deps) dirsCreateCommand() *cobra.Command {
	var raw, parent, name string
	cmd := &cobra.Command{
		Use:   "create <org>",
		Short: "Create a directory under a parent directory, or under root",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := input.Body(cmd, raw, func() (any, error) {
				if !cmd.Flags().Changed("parent-id") || !cmd.Flags().Changed("name") {
					return nil, errors.New("--parent-id and --name are required unless --body is given")
				}
				return CreateDirectory{ParentID: parent, Name: name}, nil
			})
			if err != nil {
				return err
			}
			res, err := d.newClient().CreateDirectory(cmd.Context(), args[0], body)
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
	cmd.Flags().StringVar(&parent, "parent-id", "", "the parent directory's id, or root; sent as parent_id")
	cmd.Flags().StringVar(&name, "name", "", "the new directory's name")
	cmd.Flags().StringVar(&raw, "body", "", "the request body as JSON, sent verbatim in place of the field flags")
	cmd.MarkFlagsMutuallyExclusive("body", "parent-id")
	cmd.MarkFlagsMutuallyExclusive("body", "name")
	return cmd
}

// dirsGetCommand is GET Documents/{org}/directories/{id}: the directory's
// metadata, with its path from the root.
func (d deps) dirsGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get <org> <dir>",
		Short: "Read one directory, by id or root",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := d.newClient().Directory(cmd.Context(), args[0], args[1])
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

// dirsListCommand is GET Documents/{org}/directories/{id}/directories under
// the read grammar: a directory's child directories, a page at a time.
func (d deps) dirsListCommand() *cobra.Command {
	var read readFlags
	cmd := &cobra.Command{
		Use:   "list <org> <dir>",
		Short: "List a directory's child directories, one page at a time",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			query, err := read.query(cmd)
			if err != nil {
				return err
			}
			res, err := d.newClient().ListDirectories(cmd.Context(), args[0], args[1], query...)
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
	read.bind(cmd)
	return cmd
}

// dirsMoveCommand is POST Documents/{org}/directories/{id}/move under
// If-Match. The body is --body verbatim, or MoveDirectory built from
// --parent-id and --name, both required since the service defaults neither;
// the version is resolved by input.GuardedBody.
func (d deps) dirsMoveCommand() *cobra.Command {
	var (
		raw, parent, name string
		version           int64
	)
	cmd := &cobra.Command{
		Use:   "move <org> <dir>",
		Short: "Move or rename a directory",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, ifMatch, err := input.GuardedBody(cmd, raw, version, func() (any, error) {
				if !cmd.Flags().Changed("parent-id") || !cmd.Flags().Changed("name") {
					return nil, errors.New("--parent-id and --name are required unless --body is given")
				}
				return MoveDirectory{ParentID: parent, Name: name}, nil
			})
			if err != nil {
				return err
			}
			res, err := d.newClient().MoveDirectory(cmd.Context(), args[0], args[1], ifMatch, body)
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
	cmd.Flags().StringVar(&parent, "parent-id", "", "the new parent directory's id, or root; sent as parent_id")
	cmd.Flags().StringVar(&name, "name", "", "the directory's name after the move, the current one for a move alone")
	cmd.Flags().Int64Var(&version, "version", 0, "the version the directory was last seen at, sent as If-Match")
	cmd.Flags().StringVar(&raw, "body", "", `the request body as JSON, sent verbatim in place of the field flags; a top-level "version" stands in for --version`)
	cmd.MarkFlagsMutuallyExclusive("body", "parent-id")
	cmd.MarkFlagsMutuallyExclusive("body", "name")
	return cmd
}

// dirsDeleteCommand is DELETE Documents/{org}/directories/{id} under
// If-Match. There is no body, so --version is simply required. The service
// removes an empty directory, 204, and refuses one that is not; --recursive
// asks it to mark the whole branch deleting instead, answered 202 with the
// directory's read as the Location, and the sweep removes the branch after.
// --wait then polls that Location until it answers 404, for at most the
// duration given.
func (d deps) dirsDeleteCommand() *cobra.Command {
	var (
		recursive bool
		version   int64
		wait      time.Duration
	)
	cmd := &cobra.Command{
		Use:   "delete <org> <dir>",
		Short: "Delete an empty directory, or with --recursive mark its branch for the sweep",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("wait") && !recursive {
				return errors.New("--wait needs --recursive: only a recursive delete is swept")
			}
			if wait < 0 {
				return fmt.Errorf("--wait %s: want a positive duration", wait)
			}
			client := d.newClient()
			res, err := client.DeleteDirectory(cmd.Context(), args[0], args[1], version, recursive)
			if err != nil {
				return err
			}
			if !recursive {
				if err := output.Expect(res, http.StatusNoContent); err != nil {
					return err
				}
				d.out.Response(res.Status, res.Body)
				return nil
			}
			if err := output.Expect(res, http.StatusAccepted); err != nil {
				return err
			}
			location := res.Header.Get("Location")
			if location == "" {
				return errors.New("the service answered 202 with no Location to follow")
			}
			d.out.Status(res.Status,
				"Location: "+location,
				"the branch is deleting; the sweep removes it, and the Location answers 404 once it has")
			if wait == 0 {
				return nil
			}
			took, err := AwaitSweep(cmd.Context(), client, location, wait)
			if err != nil {
				return err
			}
			d.out.Status(http.StatusNotFound, fmt.Sprintf("the sweep removed the branch within %s", took.Round(time.Millisecond)))
			return nil
		},
	}
	cmd.Flags().Int64Var(&version, "version", 0, "the version the directory was last seen at, sent as If-Match")
	_ = cmd.MarkFlagRequired("version")
	cmd.Flags().BoolVar(&recursive, "recursive", false, "mark the directory and everything under it deleting, for the sweep to remove")
	cmd.Flags().DurationVar(&wait, "wait", 0, "with --recursive, poll the Location until the sweep has removed the branch, for at most this long (e.g. 30s)")
	return cmd
}

// pollInterval is how long AwaitSweep waits between reads of the Location.
// The delete nudges the sweep, so a small branch is usually gone by the
// first or second read.
const pollInterval = 250 * time.Millisecond

// AwaitSweep reads location until it answers 404, the sweep having removed
// the branch, and returns how long that took. A 200 is the directory still
// deleting, read again after pollInterval; any other status is returned as
// output.Expect's error. When limit passes first, or ctx ends, it returns
// the reason. It is exported because it is --wait's one definition: the
// storage demo waits for the sweep through it rather than restating it.
func AwaitSweep(ctx context.Context, client *Client, location string, limit time.Duration) (time.Duration, error) {
	start := time.Now()
	deadline := start.Add(limit)
	for {
		res, err := client.DirectoryAt(ctx, location)
		if err != nil {
			return 0, err
		}
		if res.Status == http.StatusNotFound {
			return time.Since(start), nil
		}
		if err := output.Expect(res, http.StatusOK); err != nil {
			return 0, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return 0, fmt.Errorf("%s still answers 200 after %s: the sweep has not removed the branch yet", location, limit)
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(min(pollInterval, remaining)):
		}
	}
}

// filesListCommand is GET Documents/{org}/directories/{id}/files under the
// read grammar: the files in a directory, a page at a time.
func (d deps) filesListCommand() *cobra.Command {
	var read readFlags
	cmd := &cobra.Command{
		Use:   "list <org> <dir>",
		Short: "List the files in a directory, one page at a time",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			query, err := read.query(cmd)
			if err != nil {
				return err
			}
			res, err := d.newClient().ListFiles(cmd.Context(), args[0], args[1], query...)
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
	read.bind(cmd)
	return cmd
}

// filesPutCommand is POST Documents/{org}/directories/{id}/files?name={name}
// with the file's bytes as the raw body; the command keeps the name put,
// the verb a user reaches for, though the upload is a POST. The stored
// name is --name, or the
// file's base name; the Content-Type is --content-type, or the one the
// extension names, or application/octet-stream, since the service accepts
// any type.
func (d deps) filesPutCommand() *cobra.Command {
	var name, contentType string
	cmd := &cobra.Command{
		Use:   "put <org> <dir> <file>",
		Short: "Upload a file into a directory",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := os.ReadFile(args[2])
			if err != nil {
				return err
			}
			stored, media := name, contentType
			if stored == "" {
				stored = filepath.Base(args[2])
			}
			if media == "" {
				if media = mime.TypeByExtension(filepath.Ext(args[2])); media == "" {
					media = octetStream
				}
			}
			res, err := d.newClient().UploadFile(cmd.Context(), args[0], args[1], stored, media, body)
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
	cmd.Flags().StringVar(&name, "name", "", "the name the file is stored under; the file's base name when unset")
	cmd.Flags().StringVar(&contentType, "content-type", "", "the media type sent; the file extension's when unset, else "+octetStream)
	return cmd
}

// filesShowCommand is GET Documents/{org}/files/{id}: the file's metadata,
// never its bytes.
func (d deps) filesShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <org> <file-id>",
		Short: "Read one file's metadata",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := d.newClient().File(cmd.Context(), args[0], args[1])
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

// filesGetCommand is GET Documents/{org}/files/{id}/content. It prints the
// status and the object headers, Content-Disposition's filename among them,
// and writes the bytes to --out rather than to the terminal.
// --if-none-match sends a revalidation, answered 304 when the file is
// unchanged.
func (d deps) filesGetCommand() *cobra.Command {
	var out, etag string
	cmd := &cobra.Command{
		Use:   "get <org> <file-id>",
		Short: "Read a file's content: its headers, and its bytes to --out",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := d.newClient().Content(cmd.Context(), args[0], args[1], etag)
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
			d.out.Object(res.Status, res.Header, len(res.Body), saved)
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "the file the content's bytes are written to")
	cmd.Flags().StringVar(&etag, "if-none-match", "", "an ETag a previous get printed; 304 when the file is unchanged")
	return cmd
}

// filesMoveCommand is POST Documents/{org}/files/{id}/move under If-Match.
// The body is --body verbatim, or MoveFile built from --directory-id and
// --name, both required; the version is resolved by input.GuardedBody.
func (d deps) filesMoveCommand() *cobra.Command {
	var (
		raw, directory, name string
		version              int64
	)
	cmd := &cobra.Command{
		Use:   "move <org> <file-id>",
		Short: "Move or rename a file",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, ifMatch, err := input.GuardedBody(cmd, raw, version, func() (any, error) {
				if !cmd.Flags().Changed("directory-id") || !cmd.Flags().Changed("name") {
					return nil, errors.New("--directory-id and --name are required unless --body is given")
				}
				return MoveFile{DirectoryID: directory, Name: name}, nil
			})
			if err != nil {
				return err
			}
			res, err := d.newClient().MoveFile(cmd.Context(), args[0], args[1], ifMatch, body)
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
	cmd.Flags().StringVar(&directory, "directory-id", "", "the destination directory's id, or root; sent as directory_id")
	cmd.Flags().StringVar(&name, "name", "", "the file's name after the move, the current one for a move alone")
	cmd.Flags().Int64Var(&version, "version", 0, "the version the file was last seen at, sent as If-Match")
	cmd.Flags().StringVar(&raw, "body", "", `the request body as JSON, sent verbatim in place of the field flags; a top-level "version" stands in for --version`)
	cmd.MarkFlagsMutuallyExclusive("body", "directory-id")
	cmd.MarkFlagsMutuallyExclusive("body", "name")
	return cmd
}

// filesDeleteCommand is DELETE Documents/{org}/files/{id} under If-Match.
// There is no body, so --version is simply required.
func (d deps) filesDeleteCommand() *cobra.Command {
	var version int64
	cmd := &cobra.Command{
		Use:   "delete <org> <file-id>",
		Short: "Delete a file",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := d.newClient().DeleteFile(cmd.Context(), args[0], args[1], version)
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
	cmd.Flags().Int64Var(&version, "version", 0, "the version the file was last seen at, sent as If-Match")
	_ = cmd.MarkFlagRequired("version")
	return cmd
}
