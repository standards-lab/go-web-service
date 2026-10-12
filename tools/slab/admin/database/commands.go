package database

import (
	"context"
	"errors"
	"net/http"

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

// Commands builds the database command, on go-cli-sdk: the schema
// subcommand over the migration operations, and one subcommand per
// remaining endpoint. The database command declares with Use the two nodes
// every leaf below it needs, out's configuration node and client, the HTTP
// client bound to the service's base URL; the dispatcher builds them only
// when a leaf runs, and a leaf reads the client through its Invocation then.
//
// A leaf checks its input in Validate, before anything is built, so a
// missing field flag is a usage error. A leaf's Run sends one request,
// checks for 200 with output.Expect, and renders the reply through out; the
// error it returns, such as the output.Report of a problem response, is
// printed once by the dispatcher.
func Commands(client *graph.Node[*httpx.Client], out output.Source) *cli.Command {
	d := deps{client: client, out: out}
	database := &cli.Command{
		Name:    "database",
		Summary: "Call the database admin service's endpoints",
	}
	return database.Add(
		d.schemaCommands(),
		d.seedCommand(),
		d.stateCommand(),
		d.fixedCommand("diagnostics", "Read the database's health: dialect, ping, server version, and pool counters", (*Client).Diagnose),
		d.fixedCommand("patterns", "Read the pattern catalog", (*Client).Catalog),
		d.fixedCommand("statements", "Read the statements registry, every domain's compiled inventory", (*Client).Statements),
		d.fixedCommand("states", "List the named states the seeder declares", (*Client).States),
	).Use(out.Node, client)
}

// newClient returns the database client over the HTTP client the
// dispatcher built for inv.
func (d deps) newClient(inv *cli.Invocation) *Client {
	return NewClient(inv.Get(d.client))
}

// respond finishes a leaf's Run: it returns err, the request's error, when
// there is one, then output.Expect's error when res does not carry 200,
// and otherwise renders the reply through Response. Every database
// endpoint answers 200 on success.
func (d deps) respond(inv *cli.Invocation, res *httpx.Response, err error) error {
	if err != nil {
		return err
	}
	if err := output.Expect(res, http.StatusOK); err != nil {
		return err
	}
	d.out.New(inv).Response(res.Status, res.Body)
	return nil
}

// fixedCommand builds the leaf for an endpoint that takes no input: it
// accepts no arguments, and its Run sends the one request op, a Client
// method expression, stands for.
func (d deps) fixedCommand(name, summary string, op func(*Client, context.Context) (*httpx.Response, error)) *cli.Command {
	return &cli.Command{
		Name:    name,
		Summary: summary,
		Args:    cli.NoArgs,
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := op(d.newClient(inv), ctx)
			return d.respond(inv, res, err)
		},
	}
}

// schemaCommands builds the schema subcommand: the migration operations,
// each answering with the schema's resulting status.
func (d deps) schemaCommands() *cli.Command {
	schema := &cli.Command{
		Name:    "schema",
		Summary: "Read and operate on the schema's migration history",
	}
	return schema.Add(
		d.fixedCommand("status", "Read the schema's status against the migration set", (*Client).Status),
		d.fixedCommand("verify", "Check that the schema is at the set's clean head and the seeder's statements prepare against it", (*Client).Verify),
		d.fixedCommand("up", "Apply every pending migration", (*Client).Up),
		d.downCommand(),
		d.stepsCommand(),
		d.forceCommand(),
	)
}

// downCommand is POST Database/schema/down. The body is --body verbatim,
// or Down built from --set, which must be given, and --steps, omitted when
// unset: the service reverts one migration of the set then, and the default
// is left to it rather than restated here.
func (d deps) downCommand() *cli.Command {
	var (
		raw, set string
		steps    int
		body     []byte
	)
	cmd := &cli.Command{
		Name:    "down",
		Summary: "Revert a set's most recent migrations, one when --steps is unset",
		Args:    cli.NoArgs,
		Validate: func(inv *cli.Invocation) (err error) {
			body, err = input.Body(inv, raw, func() (any, error) {
				if !inv.Changed("set") {
					return nil, errSetRequired
				}
				return Down{Set: set, Steps: steps}, nil
			})
			return err
		},
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := d.newClient(inv).Down(ctx, body)
			return d.respond(inv, res, err)
		},
	}
	setFlag(cmd, &set)
	cmd.Flags().IntVar(&steps, "steps", 0, "how many migrations to revert; the service's default (1) when unset")
	bodyFlag(cmd, &raw)
	cmd.Exclusive("body", "set")
	cmd.Exclusive("body", "steps")
	return cmd
}

// errSetRequired refuses a schema operation's flags path with no --set:
// the service refuses an empty set name, so the request is not sent.
var errSetRequired = errors.New("--set is required unless --body is given; schema status lists the sets")

// setFlag declares --set, the migration set a schema operation acts on.
func setFlag(cmd *cli.Command, set *string) {
	cmd.Flags().StringVar(set, "set", "", "the migration set to act on, one of the names schema status lists")
}

// bodyFlag declares --body, the escape hatch every body-sending leaf
// offers in place of its field flags.
func bodyFlag(cmd *cli.Command, raw *string) {
	cmd.Flags().StringVar(raw, "body", "", "the request body as JSON, sent verbatim in place of the field flags")
}

// stepsCommand is POST Database/schema/steps. The body is --body verbatim,
// or Steps built from --set and --steps, both of which must be given: the
// service rejects an empty set and zero steps, so an unset flag is refused
// here rather than sent.
func (d deps) stepsCommand() *cli.Command {
	var (
		raw, set string
		steps    int
		body     []byte
	)
	cmd := &cli.Command{
		Name:    "steps",
		Summary: "Apply --steps of a set's pending migrations, or revert that many when negative",
		Args:    cli.NoArgs,
		Validate: func(inv *cli.Invocation) (err error) {
			body, err = input.Body(inv, raw, func() (any, error) {
				if !inv.Changed("set") {
					return nil, errSetRequired
				}
				if !inv.Changed("steps") {
					return nil, errors.New("--steps is required unless --body is given")
				}
				return Steps{Set: set, Steps: steps}, nil
			})
			return err
		},
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := d.newClient(inv).Steps(ctx, body)
			return d.respond(inv, res, err)
		},
	}
	setFlag(cmd, &set)
	cmd.Flags().IntVar(&steps, "steps", 0, "how many migrations to apply when positive, or revert when negative")
	bodyFlag(cmd, &raw)
	cmd.Exclusive("body", "set")
	cmd.Exclusive("body", "steps")
	return cmd
}

// forceCommand is POST Database/schema/force. The body is --body verbatim,
// or Force built from --set and --version, both of which must be given: 0
// is a meaningful version (an empty history), so an unset flag is refused
// rather than sent.
func (d deps) forceCommand() *cli.Command {
	var (
		raw, set string
		version  int
		body     []byte
	)
	cmd := &cli.Command{
		Name:    "force",
		Summary: "Set a set's migration history to --version without running any file",
		Args:    cli.NoArgs,
		Validate: func(inv *cli.Invocation) (err error) {
			body, err = input.Body(inv, raw, func() (any, error) {
				if !inv.Changed("set") {
					return nil, errSetRequired
				}
				if !inv.Changed("version") {
					return nil, errors.New("--version is required unless --body is given")
				}
				return Force{Set: set, Version: version}, nil
			})
			return err
		},
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := d.newClient(inv).Force(ctx, body)
			return d.respond(inv, res, err)
		},
	}
	setFlag(cmd, &set)
	cmd.Flags().IntVar(&version, "version", 0, "the version the history is set to; 0 empties it")
	bodyFlag(cmd, &raw)
	cmd.Exclusive("body", "set")
	cmd.Exclusive("body", "version")
	return cmd
}

// seedCommand is POST Database/seed. The body is --body verbatim, State
// built from --state when it is set, and otherwise nothing: the service
// applies its configured set when the request carries no body, and refuses
// when it configures none. Seed is additive, never a reset: it inserts a
// named set's rows idempotently over the schema as it stands, so seeding
// an empty set onto a populated database leaves the existing rows in
// place. stateCommand is the one that clears first.
func (d deps) seedCommand() *cli.Command {
	var (
		raw, state string
		body       []byte
	)
	cmd := &cli.Command{
		Name:    "seed",
		Summary: "Apply a named set's rows on top of the schema as it stands (additive, never a reset); the service's configured set when --state is unset",
		Args:    cli.NoArgs,
		Validate: func(inv *cli.Invocation) (err error) {
			body = nil
			if inv.Changed("body") || inv.Changed("state") {
				body, err = input.Body(inv, raw, func() (any, error) {
					return State{State: state}, nil
				})
			}
			return err
		},
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := d.newClient(inv).Seed(ctx, body)
			return d.respond(inv, res, err)
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "the state whose set to apply; the service's configured set when unset")
	bodyFlag(cmd, &raw)
	cmd.Exclusive("body", "state")
	return cmd
}

// stateCommand is POST Database/state. The body is --body verbatim, or
// Reset built from --state, which must be given: the service refuses an
// empty name, so an unset flag is refused here before the request fires.
// --confirm is sent as given: the service refuses an unconfirmed reset, and
// that refusal is the service's to state, not restated here.
func (d deps) stateCommand() *cli.Command {
	var (
		raw, state string
		confirm    bool
		body       []byte
	)
	cmd := &cli.Command{
		Name:    "state",
		Summary: "Reset the database to a named state: revert every migration set, apply the sets, and seed; requires --confirm",
		Args:    cli.NoArgs,
		Validate: func(inv *cli.Invocation) (err error) {
			body, err = input.Body(inv, raw, func() (any, error) {
				if !inv.Changed("state") {
					return nil, errors.New("--state is required unless --body is given")
				}
				return Reset{State: state, Confirm: confirm}, nil
			})
			return err
		},
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := d.newClient(inv).Reset(ctx, body)
			return d.respond(inv, res, err)
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "the state to reset to, one of the names states lists")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "confirm the reset, which reverts every migration set and its rows")
	bodyFlag(cmd, &raw)
	cmd.Exclusive("body", "state")
	cmd.Exclusive("body", "confirm")
	return cmd
}
