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

// Commands builds the database command, on go-cli-sdk: the schema
// subcommand over the migration operations, and one subcommand per
// remaining endpoint, and returns it alone in the slice the composition
// root mounts with admin.Add(Commands(...)...). The database command declares with Use the two nodes
// every leaf below it needs, out's color node and client, the HTTP
// client bound to the service's base URL; the dispatcher builds them only
// when a leaf runs, and a leaf reads the client through its Invocation then.
//
// A leaf checks its input in Validate, before anything is built, so a
// missing field flag is a usage error. A leaf's Run sends one request,
// checks for 200 with output.Expect, and renders the reply through out; the
// error it returns, such as the output.Report of a problem response, is
// printed once by the dispatcher.
func Commands(client *graph.Node[*httpx.Client], out output.Source) []*cli.Command {
	database := &cli.Command{
		Name:    "database",
		Summary: "Call the database admin service's endpoints",
	}
	database.Add(
		schemaCommands(client, out),
		seedCommand(client, out),
		stateCommand(client, out),
		fixedCommand(client, out, "diagnostics", "Read the database's health: dialect, ping, server version, and pool counters", (*Client).Diagnose),
		fixedCommand(client, out, "patterns", "Read the pattern catalog", (*Client).Catalog),
		fixedCommand(client, out, "statements", "Read the statements registry, every domain's compiled inventory", (*Client).Statements),
		fixedCommand(client, out, "states", "List the named states the seeder declares", (*Client).States),
	).Use(out.Node, client)
	return []*cli.Command{database}
}

// fixedCommand builds the leaf for an endpoint that takes no input: it
// accepts no arguments, and its Run sends the one request op, a Client
// method expression, stands for.
func fixedCommand(client *graph.Node[*httpx.Client], out output.Source, name, summary string, op func(*Client, context.Context) (*httpx.Response, error)) *cli.Command {
	return &cli.Command{
		Name:    name,
		Summary: summary,
		Args:    cli.NoArgs,
		Run: func(ctx context.Context, inv *cli.Invocation) error {
			res, err := op(NewClient(inv.Get(client)), ctx)
			return out.Respond(inv, res, err, http.StatusOK)
		},
	}
}

// schemaCommands builds the schema subcommand: the migration operations,
// each answering with the schema's resulting status.
func schemaCommands(client *graph.Node[*httpx.Client], out output.Source) *cli.Command {
	schema := &cli.Command{
		Name:    "schema",
		Summary: "Read and operate on the schema's migration history",
	}
	return schema.Add(
		fixedCommand(client, out, "status", "Read the schema's status against the migration set", (*Client).Status),
		fixedCommand(client, out, "verify", "Check that the schema is at the set's clean head and the seeder's statements prepare against it", (*Client).Verify),
		fixedCommand(client, out, "up", "Apply every pending migration", (*Client).Up),
		downCommand(client, out),
		stepsCommand(client, out),
		forceCommand(client, out),
	)
}

// downCommand is POST Database/schema/down. The body is --body verbatim,
// or Down built from --set, which must be given, and --steps, omitted when
// unset: the service reverts one migration of the set then, and the default
// is left to it rather than restated here.
func downCommand(client *graph.Node[*httpx.Client], out output.Source) *cli.Command {
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
			res, err := NewClient(inv.Get(client)).Down(ctx, body)
			return out.Respond(inv, res, err, http.StatusOK)
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
func stepsCommand(client *graph.Node[*httpx.Client], out output.Source) *cli.Command {
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
			res, err := NewClient(inv.Get(client)).Steps(ctx, body)
			return out.Respond(inv, res, err, http.StatusOK)
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
func forceCommand(client *graph.Node[*httpx.Client], out output.Source) *cli.Command {
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
			res, err := NewClient(inv.Get(client)).Force(ctx, body)
			return out.Respond(inv, res, err, http.StatusOK)
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
func seedCommand(client *graph.Node[*httpx.Client], out output.Source) *cli.Command {
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
			res, err := NewClient(inv.Get(client)).Seed(ctx, body)
			return out.Respond(inv, res, err, http.StatusOK)
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
func stateCommand(client *graph.Node[*httpx.Client], out output.Source) *cli.Command {
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
			res, err := NewClient(inv.Get(client)).Reset(ctx, body)
			return out.Respond(inv, res, err, http.StatusOK)
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "the state to reset to, one of the names states lists")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "confirm the reset, which reverts every migration set and its rows")
	bodyFlag(cmd, &raw)
	cmd.Exclusive("body", "state")
	cmd.Exclusive("body", "confirm")
	return cmd
}
