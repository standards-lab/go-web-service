package document

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
	"github.com/standards-lab/go-web-service/tools/slab/input"
	"github.com/standards-lab/go-web-service/tools/slab/output"
)

// octetStream is the media type an upload is sent under when its file's
// extension names none and --content-type is unset. The service accepts any
// type, so an unknown extension is no reason to refuse.
const octetStream = "application/octet-stream"

// Commands builds the docs command with a dirs group and a files group, one
// leaf per endpoint, on go-cli-sdk, and returns it alone in the slice the
// composition root mounts with root.Add(Commands(...)...). The docs command declares with Use the
// two nodes every leaf below it needs, out's configuration node and client,
// the HTTP client bound to the service's base URL; the dispatcher builds
// them only when a leaf runs, and a leaf reads the client through its
// Invocation then.
//
// A leaf checks its input in Validate, before anything is built, so a
// missing field flag, version, or a --wait without --recursive is a usage
// error. A leaf's Run sends its request, checks the status it expects with
// output.Expect, and renders the reply through out; the error it returns,
// such as the output.Report of a problem response, is printed once by the
// dispatcher.
func Commands(client *graph.Node[*httpx.Client], out output.Source) []*cli.Command {
	dirs := &cli.Command{
		Name:    "dirs",
		Summary: "Create, read, list, move, and delete an organization's directories",
	}
	dirs.Add(
		dirsCreateCommand(client, out),
		dirsGetCommand(client, out),
		dirsListCommand(client, out),
		dirsMoveCommand(client, out),
		dirsDeleteCommand(client, out),
	)
	files := &cli.Command{
		Name:    "files",
		Summary: "List, upload, read, move, and delete an organization's files",
	}
	files.Add(
		filesListCommand(client, out),
		filesPutCommand(client, out),
		filesShowCommand(client, out),
		filesGetCommand(client, out),
		filesMoveCommand(client, out),
		filesDeleteCommand(client, out),
	)
	docs := &cli.Command{
		Name:    "docs",
		Summary: "Call the document domain's endpoints",
	}
	docs.Add(dirs, files).Use(out.Node, client)
	return []*cli.Command{docs}
}

// dirsCreateCommand is POST Documents/{org}/directories. The body is --body
// verbatim, or CreateDirectory built from --parent-id and --name.
func dirsCreateCommand(client *graph.Node[*httpx.Client], out output.Source) *cli.Command {
	var (
		raw, parent, name string
		body              []byte
	)
	cmd := &cli.Command{
		Name:     "create",
		Summary:  "Create a directory under a parent directory, or under root",
		Synopsis: "<org>",
		Args:     cli.ExactArgs(1),
		Validate: func(inv *cli.Invocation) (err error) {
			body, err = input.Body(inv, raw, func() (any, error) {
				if !inv.Changed("parent-id") || !inv.Changed("name") {
					return nil, errors.New("--parent-id and --name are required unless --body is given")
				}
				return CreateDirectory{ParentID: parent, Name: name}, nil
			})
			return err
		},
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := NewClient(inv.Get(client)).CreateDirectory(ctx, inv.Args[0], body)
			return out.Respond(inv, res, err, http.StatusCreated)
		},
	}
	fs := cmd.Flags()
	fs.StringVar(&parent, "parent-id", "", "the parent directory's id, or root; sent as parent_id")
	fs.StringVar(&name, "name", "", "the new directory's name")
	fs.StringVar(&raw, "body", "", "the request body as JSON, sent verbatim in place of the field flags")
	cmd.Exclusive("body", "parent-id")
	cmd.Exclusive("body", "name")
	return cmd
}

// dirsGetCommand is GET Documents/{org}/directories/{id}: the directory's
// metadata, with its path from the root.
func dirsGetCommand(client *graph.Node[*httpx.Client], out output.Source) *cli.Command {
	return &cli.Command{
		Name:     "get",
		Summary:  "Read one directory, by id or root",
		Synopsis: "<org> <dir>",
		Args:     cli.ExactArgs(2),
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := NewClient(inv.Get(client)).Directory(ctx, inv.Args[0], inv.Args[1])
			return out.Respond(inv, res, err, http.StatusOK)
		},
	}
}

// dirsListCommand is GET Documents/{org}/directories/{id}/directories under
// the read grammar: a directory's child directories, a page at a time.
func dirsListCommand(client *graph.Node[*httpx.Client], out output.Source) *cli.Command {
	var (
		read  input.ReadFlags
		query [][2]string
	)
	cmd := &cli.Command{
		Name:     "list",
		Summary:  "List a directory's child directories, one page at a time",
		Synopsis: "<org> <dir>",
		Args:     cli.ExactArgs(2),
		Validate: func(inv *cli.Invocation) (err error) {
			query, err = read.Query(inv)
			return err
		},
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := NewClient(inv.Get(client)).ListDirectories(ctx, inv.Args[0], inv.Args[1], query...)
			return out.Respond(inv, res, err, http.StatusOK)
		},
	}
	read.Bind(cmd)
	return cmd
}

// dirsMoveCommand is POST Documents/{org}/directories/{id}/move under
// If-Match. The body is --body verbatim, or MoveDirectory built from
// --parent-id and --name, both required since the service defaults neither;
// the version is resolved by input.GuardedBody.
func dirsMoveCommand(client *graph.Node[*httpx.Client], out output.Source) *cli.Command {
	var (
		raw, parent, name string
		version, ifMatch  int64
		body              []byte
	)
	cmd := &cli.Command{
		Name:     "move",
		Summary:  "Move or rename a directory",
		Synopsis: "<org> <dir>",
		Args:     cli.ExactArgs(2),
		Validate: func(inv *cli.Invocation) (err error) {
			body, ifMatch, err = input.GuardedBody(inv, raw, version, func() (any, error) {
				if !inv.Changed("parent-id") || !inv.Changed("name") {
					return nil, errors.New("--parent-id and --name are required unless --body is given")
				}
				return MoveDirectory{ParentID: parent, Name: name}, nil
			})
			return err
		},
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := NewClient(inv.Get(client)).MoveDirectory(ctx, inv.Args[0], inv.Args[1], ifMatch, body)
			return out.Respond(inv, res, err, http.StatusOK)
		},
	}
	fs := cmd.Flags()
	fs.StringVar(&parent, "parent-id", "", "the new parent directory's id, or root; sent as parent_id")
	fs.StringVar(&name, "name", "", "the directory's name after the move, the current one for a move alone")
	fs.Int64Var(&version, "version", 0, "the version the directory was last seen at, sent as If-Match")
	fs.StringVar(&raw, "body", "", `the request body as JSON, sent verbatim in place of the field flags; a top-level "version" stands in for --version`)
	cmd.Exclusive("body", "parent-id")
	cmd.Exclusive("body", "name")
	return cmd
}

// dirsDeleteCommand is DELETE Documents/{org}/directories/{id} under
// If-Match. There is no body, so --version is simply required. The service
// removes an empty directory, 204, and refuses one that is not; --recursive
// asks it to mark the whole branch deleting instead, answered 202 with the
// directory's read as the Location, and the sweep removes the branch after.
// --wait then polls that Location until it answers 404, for at most the
// duration given. A --wait without --recursive, or a negative one, is
// refused in Validate.
func dirsDeleteCommand(client *graph.Node[*httpx.Client], out output.Source) *cli.Command {
	var (
		recursive bool
		version   int64
		wait      time.Duration
	)
	cmd := &cli.Command{
		Name:     "delete",
		Summary:  "Delete an empty directory, or with --recursive mark its branch for the sweep",
		Synopsis: "<org> <dir>",
		Args:     cli.ExactArgs(2),
		Validate: func(inv *cli.Invocation) error {
			if inv.Changed("wait") && !recursive {
				return errors.New("--wait needs --recursive: only a recursive delete is swept")
			}
			if wait < 0 {
				return fmt.Errorf("--wait %s: want a positive duration", wait)
			}
			return nil
		},
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			docs := NewClient(inv.Get(client))
			res, err := docs.DeleteDirectory(ctx, inv.Args[0], inv.Args[1], version, recursive)
			if !recursive {
				return out.Respond(inv, res, err, http.StatusNoContent)
			}
			if err != nil {
				return err
			}
			if err := output.Expect(res, http.StatusAccepted); err != nil {
				return err
			}
			location := res.Header.Get("Location")
			if location == "" {
				return errors.New("the service answered 202 with no Location to follow")
			}
			o := out.New(inv)
			o.Status(res.Status,
				"Location: "+location,
				"the branch is deleting; the sweep removes it, and the Location answers 404 once it has")
			if wait == 0 {
				return nil
			}
			took, err := AwaitSweep(ctx, docs, location, wait)
			if err != nil {
				return err
			}
			o.Status(http.StatusNotFound, fmt.Sprintf("the sweep removed the branch within %s", took.Round(time.Millisecond)))
			return nil
		},
	}
	fs := cmd.Flags()
	fs.Int64Var(&version, "version", 0, "the version the directory was last seen at, sent as If-Match")
	fs.BoolVar(&recursive, "recursive", false, "mark the directory and everything under it deleting, for the sweep to remove")
	fs.DurationVar(&wait, "wait", 0, "with --recursive, poll the Location until the sweep has removed the branch, for at most this long (e.g. 30s)")
	cmd.Require("version")
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
func filesListCommand(client *graph.Node[*httpx.Client], out output.Source) *cli.Command {
	var (
		read  input.ReadFlags
		query [][2]string
	)
	cmd := &cli.Command{
		Name:     "list",
		Summary:  "List the files in a directory, one page at a time",
		Synopsis: "<org> <dir>",
		Args:     cli.ExactArgs(2),
		Validate: func(inv *cli.Invocation) (err error) {
			query, err = read.Query(inv)
			return err
		},
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := NewClient(inv.Get(client)).ListFiles(ctx, inv.Args[0], inv.Args[1], query...)
			return out.Respond(inv, res, err, http.StatusOK)
		},
	}
	read.Bind(cmd)
	return cmd
}

// filesPutCommand is POST Documents/{org}/directories/{id}/files?name={name}
// with the file's bytes as the raw body; the command keeps the name put,
// the verb a user reaches for, though the upload is a POST. The stored name
// is --name, or the file's base name; the Content-Type is --content-type,
// or the one the extension names, or application/octet-stream, since the
// service accepts any type. Both are resolved from the file's name alone;
// the file is read in Run, so a file that cannot be read is a failure, not
// a usage error.
func filesPutCommand(client *graph.Node[*httpx.Client], out output.Source) *cli.Command {
	var name, contentType string
	cmd := &cli.Command{
		Name:     "put",
		Summary:  "Upload a file into a directory",
		Synopsis: "<org> <dir> <file>",
		Args:     cli.ExactArgs(3),
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			path := inv.Args[2]
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			stored, media := name, contentType
			if stored == "" {
				stored = filepath.Base(path)
			}
			if media == "" {
				if media = mime.TypeByExtension(filepath.Ext(path)); media == "" {
					media = octetStream
				}
			}
			res, err := NewClient(inv.Get(client)).UploadFile(ctx, inv.Args[0], inv.Args[1], stored, media, body)
			return out.Respond(inv, res, err, http.StatusCreated)
		},
	}
	fs := cmd.Flags()
	fs.StringVar(&name, "name", "", "the name the file is stored under; the file's base name when unset")
	fs.StringVar(&contentType, "content-type", "", "the media type sent; the file extension's when unset, else "+octetStream)
	return cmd
}

// filesShowCommand is GET Documents/{org}/files/{id}: the file's metadata,
// never its bytes.
func filesShowCommand(client *graph.Node[*httpx.Client], out output.Source) *cli.Command {
	return &cli.Command{
		Name:     "show",
		Summary:  "Read one file's metadata",
		Synopsis: "<org> <file-id>",
		Args:     cli.ExactArgs(2),
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := NewClient(inv.Get(client)).File(ctx, inv.Args[0], inv.Args[1])
			return out.Respond(inv, res, err, http.StatusOK)
		},
	}
}

// filesGetCommand is GET Documents/{org}/files/{id}/content. It prints the
// status and the object headers, Content-Disposition's filename among them,
// and writes the bytes to --out rather than to the terminal.
// --if-none-match sends a revalidation, answered 304 when the file is
// unchanged.
func filesGetCommand(client *graph.Node[*httpx.Client], out output.Source) *cli.Command {
	var dest, etag string
	cmd := &cli.Command{
		Name:     "get",
		Summary:  "Read a file's content: its headers, and its bytes to --out",
		Synopsis: "<org> <file-id>",
		Args:     cli.ExactArgs(2),
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := NewClient(inv.Get(client)).Content(ctx, inv.Args[0], inv.Args[1], etag)
			if err != nil {
				return err
			}
			if res.Status != http.StatusNotModified {
				if err := output.Expect(res, http.StatusOK); err != nil {
					return err
				}
			}
			saved := ""
			if dest != "" && res.Status == http.StatusOK {
				if err := os.WriteFile(dest, res.Body, 0o644); err != nil {
					return err
				}
				saved = dest
			}
			out.New(inv).Object(res.Status, res.Header, len(res.Body), saved)
			return nil
		},
	}
	cmd.Flags().StringVar(&dest, "out", "", "the file the content's bytes are written to")
	cmd.Flags().StringVar(&etag, "if-none-match", "", "an ETag a previous get printed; 304 when the file is unchanged")
	return cmd
}

// filesMoveCommand is POST Documents/{org}/files/{id}/move under If-Match.
// The body is --body verbatim, or MoveFile built from --directory-id and
// --name, both required; the version is resolved by input.GuardedBody.
func filesMoveCommand(client *graph.Node[*httpx.Client], out output.Source) *cli.Command {
	var (
		raw, directory, name string
		version, ifMatch     int64
		body                 []byte
	)
	cmd := &cli.Command{
		Name:     "move",
		Summary:  "Move or rename a file",
		Synopsis: "<org> <file-id>",
		Args:     cli.ExactArgs(2),
		Validate: func(inv *cli.Invocation) (err error) {
			body, ifMatch, err = input.GuardedBody(inv, raw, version, func() (any, error) {
				if !inv.Changed("directory-id") || !inv.Changed("name") {
					return nil, errors.New("--directory-id and --name are required unless --body is given")
				}
				return MoveFile{DirectoryID: directory, Name: name}, nil
			})
			return err
		},
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := NewClient(inv.Get(client)).MoveFile(ctx, inv.Args[0], inv.Args[1], ifMatch, body)
			return out.Respond(inv, res, err, http.StatusOK)
		},
	}
	fs := cmd.Flags()
	fs.StringVar(&directory, "directory-id", "", "the destination directory's id, or root; sent as directory_id")
	fs.StringVar(&name, "name", "", "the file's name after the move, the current one for a move alone")
	fs.Int64Var(&version, "version", 0, "the version the file was last seen at, sent as If-Match")
	fs.StringVar(&raw, "body", "", `the request body as JSON, sent verbatim in place of the field flags; a top-level "version" stands in for --version`)
	cmd.Exclusive("body", "directory-id")
	cmd.Exclusive("body", "name")
	return cmd
}

// filesDeleteCommand is DELETE Documents/{org}/files/{id} under If-Match.
// There is no body, so --version is simply required.
func filesDeleteCommand(client *graph.Node[*httpx.Client], out output.Source) *cli.Command {
	var version int64
	cmd := &cli.Command{
		Name:     "delete",
		Summary:  "Delete a file",
		Synopsis: "<org> <file-id>",
		Args:     cli.ExactArgs(2),
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := NewClient(inv.Get(client)).DeleteFile(ctx, inv.Args[0], inv.Args[1], version)
			return out.Respond(inv, res, err, http.StatusNoContent)
		},
	}
	cmd.Flags().Int64Var(&version, "version", 0, "the version the file was last seen at, sent as If-Match")
	cmd.Require("version")
	return cmd
}
