package database

import (
	"context"
	"errors"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/httpx"
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

// Commands builds the database command: the schema subcommand over the
// migration operations, and one subcommand per remaining endpoint. Each leaf
// subcommand's RunE calls newClient when it runs, not when the tree is
// built: the composition root closes newClient over its persistent flags,
// which cobra parses during execution, so the base URL is unknown until
// then. out is the output every subcommand renders its reply through,
// constructed by the same root. A subcommand returns its error unrendered;
// the root writes it through out's Error and sets the exit code.
func Commands(newClient func() *Client, out *output.Output) *cobra.Command {
	d := deps{newClient: newClient, out: out}
	database := &cobra.Command{
		Use:   "database",
		Short: "Call the database admin service's endpoints",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	database.AddCommand(
		d.schemaCommands(),
		d.seedCommand(),
		d.stateCommand(),
		d.out.FixedCommand("diagnostics", "Read the database's health: dialect, ping, server version, and pool counters", http.StatusOK, func(ctx context.Context) (*httpx.Response, error) {
			return d.newClient().Diagnose(ctx)
		}),
		d.out.FixedCommand("patterns", "Read the pattern catalog", http.StatusOK, func(ctx context.Context) (*httpx.Response, error) {
			return d.newClient().Catalog(ctx)
		}),
		d.out.FixedCommand("statements", "Read the statements registry, every domain's compiled inventory", http.StatusOK, func(ctx context.Context) (*httpx.Response, error) {
			return d.newClient().Statements(ctx)
		}),
		d.out.FixedCommand("states", "List the named states the seeder declares", http.StatusOK, func(ctx context.Context) (*httpx.Response, error) {
			return d.newClient().States(ctx)
		}),
	)
	return database
}

// schemaCommands builds the schema subcommand: the migration operations,
// each answering with the schema's resulting status.
func (d deps) schemaCommands() *cobra.Command {
	schema := &cobra.Command{
		Use:   "schema",
		Short: "Read and operate on the schema's migration history",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	schema.AddCommand(
		d.out.FixedCommand("status", "Read the schema's status against the migration set", http.StatusOK, func(ctx context.Context) (*httpx.Response, error) {
			return d.newClient().Status(ctx)
		}),
		d.out.FixedCommand("verify", "Check that the schema is at the set's clean head and the seeder's statements prepare against it", http.StatusOK, func(ctx context.Context) (*httpx.Response, error) {
			return d.newClient().Verify(ctx)
		}),
		d.out.FixedCommand("up", "Apply every pending migration", http.StatusOK, func(ctx context.Context) (*httpx.Response, error) {
			return d.newClient().Up(ctx)
		}),
		d.downCommand(),
		d.stepsCommand(),
		d.forceCommand(),
	)
	return schema
}

// downCommand is POST Database/schema/down. The body is --body verbatim,
// Steps built from --steps when it is set, and otherwise nothing: the
// service reverts one migration when the request carries no body, and the
// default is left to it rather than restated here.
func (d deps) downCommand() *cobra.Command {
	var (
		raw   string
		steps int
	)
	cmd := &cobra.Command{
		Use:   "down",
		Short: "Revert the most recent migrations, one when --steps is unset",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var body []byte
			if cmd.Flags().Changed("body") || cmd.Flags().Changed("steps") {
				var err error
				body, err = input.Body(cmd, raw, func() (any, error) {
					return Steps{Steps: steps}, nil
				})
				if err != nil {
					return err
				}
			}
			res, err := d.newClient().Down(cmd.Context(), body)
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
	cmd.Flags().IntVar(&steps, "steps", 0, "how many migrations to revert; the service's default (1) when unset")
	cmd.Flags().StringVar(&raw, "body", "", "the request body as JSON, sent verbatim in place of the field flags")
	cmd.MarkFlagsMutuallyExclusive("body", "steps")
	return cmd
}

// stepsCommand is POST Database/schema/steps. The body is --body verbatim,
// or Steps built from --steps, which must be given: the service rejects
// zero, so an unset flag is refused here rather than sent as one.
func (d deps) stepsCommand() *cobra.Command {
	var (
		raw   string
		steps int
	)
	cmd := &cobra.Command{
		Use:   "steps",
		Short: "Apply --steps pending migrations, or revert that many when negative",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := input.Body(cmd, raw, func() (any, error) {
				if !cmd.Flags().Changed("steps") {
					return nil, errors.New("--steps is required unless --body is given")
				}
				return Steps{Steps: steps}, nil
			})
			if err != nil {
				return err
			}
			res, err := d.newClient().Steps(cmd.Context(), body)
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
	cmd.Flags().IntVar(&steps, "steps", 0, "how many migrations to apply when positive, or revert when negative")
	cmd.Flags().StringVar(&raw, "body", "", "the request body as JSON, sent verbatim in place of the field flags")
	cmd.MarkFlagsMutuallyExclusive("body", "steps")
	return cmd
}

// forceCommand is POST Database/schema/force. The body is --body verbatim,
// or Force built from --version, which must be given: 0 is a meaningful
// value (an empty history), so an unset flag is refused rather than sent.
func (d deps) forceCommand() *cobra.Command {
	var (
		raw     string
		version int
	)
	cmd := &cobra.Command{
		Use:   "force",
		Short: "Set the migration history to --version without running any file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := input.Body(cmd, raw, func() (any, error) {
				if !cmd.Flags().Changed("version") {
					return nil, errors.New("--version is required unless --body is given")
				}
				return Force{Version: version}, nil
			})
			if err != nil {
				return err
			}
			res, err := d.newClient().Force(cmd.Context(), body)
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
	cmd.Flags().IntVar(&version, "version", 0, "the version the history is set to; 0 empties it")
	cmd.Flags().StringVar(&raw, "body", "", "the request body as JSON, sent verbatim in place of the field flags")
	cmd.MarkFlagsMutuallyExclusive("body", "version")
	return cmd
}

// seedCommand is POST Database/seed. The body is --body verbatim, State
// built from --state when it is set, and otherwise nothing: the service
// applies its configured set when the request carries no body, and refuses
// when it configures none. Seed is additive, never a reset: it inserts a
// named set's rows idempotently over the schema as it stands, so seeding
// an empty set onto a populated database leaves the existing rows in
// place. stateCommand is the one that clears first.
func (d deps) seedCommand() *cobra.Command {
	var raw, state string
	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Apply a named set's rows on top of the schema as it stands (additive, never a reset); the service's configured set when --state is unset",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var body []byte
			if cmd.Flags().Changed("body") || cmd.Flags().Changed("state") {
				var err error
				body, err = input.Body(cmd, raw, func() (any, error) {
					return State{State: state}, nil
				})
				if err != nil {
					return err
				}
			}
			res, err := d.newClient().Seed(cmd.Context(), body)
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
	cmd.Flags().StringVar(&state, "state", "", "the state whose set to apply; the service's configured set when unset")
	cmd.Flags().StringVar(&raw, "body", "", "the request body as JSON, sent verbatim in place of the field flags")
	cmd.MarkFlagsMutuallyExclusive("body", "state")
	return cmd
}

// stateCommand is POST Database/state. The body is --body verbatim, or
// State built from --state, which must be given: the service refuses an
// empty name, so an unset flag is refused here before the request fires.
func (d deps) stateCommand() *cobra.Command {
	var raw, state string
	cmd := &cobra.Command{
		Use:   "state",
		Short: "Reset the database to a named state: revert every migration, apply the set, and seed it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := input.Body(cmd, raw, func() (any, error) {
				if !cmd.Flags().Changed("state") {
					return nil, errors.New("--state is required unless --body is given")
				}
				return State{State: state}, nil
			})
			if err != nil {
				return err
			}
			res, err := d.newClient().Reset(cmd.Context(), body)
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
	cmd.Flags().StringVar(&state, "state", "", "the state to reset to, one of the names states lists")
	cmd.Flags().StringVar(&raw, "body", "", "the request body as JSON, sent verbatim in place of the field flags")
	cmd.MarkFlagsMutuallyExclusive("body", "state")
	return cmd
}
