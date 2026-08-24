package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	gomigrate "github.com/golang-migrate/migrate/v4"
	pgxv5 "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/standards-lab/go-web-service/internal/config"
	"github.com/standards-lab/go-web-service/internal/infrastructure"
	"github.com/standards-lab/go-web-service/internal/process"
)

const migrateUsage = `usage: db migrate <verb> [flags]

verbs:
	up                apply every pending migration
	down [-n N]       revert N migrations, or every applied migration when N is 0
	steps -n N        migrate N relative to the current version; negative is down
	force -version V  record version V without running migrations (recovery only)
	version           print the current schema version and dirty state
`

func migrateCmd(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return process.Usage(stderr, migrateUsage)
	}

	switch args[0] {
	case "-h", "-help", "--help", "help":
		return process.Usage(stdout, migrateUsage)
	case "up":
		return migrateUp(args[1:], stdout, stderr)
	case "down":
		return migrateDown(args[1:], stdout, stderr)
	case "steps":
		return migrateSteps(args[1:], stdout, stderr)
	case "force":
		return migrateForce(args[1:], stdout, stderr)
	case "version":
		return migrateVersion(args[1:], stdout, stderr)
	default:
		return process.Usage(
			stderr,
			fmt.Sprintf("db migrate: unknown verb %q\n%s", args[0], migrateUsage),
		)
	}
}

func migrateUp(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("migrate up", flag.ContinueOnError)
	fl.SetOutput(stderr)
	if err := fl.Parse(args); err != nil {
		return process.ExitUsage
	}
	return runMigrate(stdout, stderr, "up", func(m *gomigrate.Migrate) error {
		return noChangeOK(m.Up())
	})
}

func migrateDown(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("migrate down", flag.ContinueOnError)
	fl.SetOutput(stderr)
	n := fl.Int("n", 0, "migrations to revert; 0 reverts all")
	if err := fl.Parse(args); err != nil {
		return process.ExitUsage
	}
	if *n < 0 {
		return process.Usage(stderr, "db migrate down: -n must not be negative")
	}
	return runMigrate(stdout, stderr, "down", func(m *gomigrate.Migrate) error {
		if *n == 0 {
			return noChangeOK(m.Down())
		}
		return noChangeOK(m.Steps(-*n))
	})
}

func migrateSteps(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("migrate steps", flag.ContinueOnError)
	fl.SetOutput(stderr)
	n := fl.Int("n", 0, "relative distance; negative migrates down")
	if err := fl.Parse(args); err != nil {
		return process.ExitUsage
	}
	if *n == 0 {
		return process.Usage(stderr, "db migrate steps: -n must be non-zero")
	}
	return runMigrate(stdout, stderr, "steps", func(m *gomigrate.Migrate) error {
		return noChangeOK(m.Steps(*n))
	})
}

func migrateForce(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("migrate force", flag.ContinueOnError)
	fl.SetOutput(stderr)
	version := fl.Int("version", 0, "schema version to record")
	if err := fl.Parse(args); err != nil {
		return process.ExitUsage
	}
	var set bool
	fl.Visit(func(f *flag.Flag) {
		if f.Name == "version" {
			set = true
		}
	})
	if !set {
		return process.Usage(stderr, "db migrate force: -version is required")
	}
	return runMigrate(stdout, stderr, "force", func(m *gomigrate.Migrate) error {
		return m.Force(*version)
	})
}

func migrateVersion(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("migrate version", flag.ContinueOnError)
	fl.SetOutput(stderr)
	if err := fl.Parse(args); err != nil {
		return process.ExitUsage
	}
	return withMigrator(stdout, stderr, func(m *gomigrate.Migrate) int {
		version, dirty, err := m.Version()
		if errors.Is(err, gomigrate.ErrNilVersion) {
			_, _ = fmt.Fprintln(stdout, "version: none")
			return process.ExitOK
		}
		if err != nil {
			return process.Fail(stderr, "migrate version failed", err)
		}
		_, _ = fmt.Fprintf(stdout, "version: %d (dirty: %t)\n", version, dirty)
		return process.ExitOK
	})
}

func runMigrate(
	stdout, stderr io.Writer,
	verb string,
	op func(m *gomigrate.Migrate) error,
) int {
	return withMigrator(stdout, stderr, func(m *gomigrate.Migrate) int {
		if err := op(m); err != nil {
			return process.Fail(stderr, "migrate "+verb+" failed", err)
		}
		_, _ = fmt.Fprintf(stdout, "migrate %s: ok\n", verb)
		return process.ExitOK
	})
}

func noChangeOK(err error) error {
	if errors.Is(err, gomigrate.ErrNoChange) {
		return nil
	}
	return err
}

func newMigrator(infra *infrastructure.Infrastructure) (*gomigrate.Migrate, error) {
	source, err := iofs.New(migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("open migration source: %w", err)
	}

	driver, err := pgxv5.WithInstance(infra.DB.Conn(), &pgxv5.Config{})
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("wrap migrate driver: %w", err), source.Close(),
		)
	}

	m, err := gomigrate.NewWithInstance("iofs", source, "database", driver)
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("construct migrator: %w", err), source.Close(),
		)
	}
	return m, nil
}

func withInfrastructure(
	stdout, stderr io.Writer,
	fn func(ctx context.Context, infra *infrastructure.Infrastructure) int,
) int {
	ctx, stop := process.SignalContext()
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return process.Fail(stderr, "config load failed", err)
	}

	infra, err := infrastructure.New(stdout, cfg, nil)
	if err != nil {
		return process.Fail(stderr, "infrastructure init failed", err)
	}

	if err := infra.DB.Start(ctx); err != nil {
		return process.Fail(stderr, "database start failed", err)
	}
	defer func() {
		if err := infra.DB.Shutdown(context.Background()); err != nil {
			_, _ = fmt.Fprintln(stderr, "database shutdown:", err)
		}
	}()

	return fn(ctx, infra)
}

func withMigrator(
	stdout, stderr io.Writer,
	fn func(m *gomigrate.Migrate) int,
) int {
	run := func(_ context.Context, infra *infrastructure.Infrastructure) int {
		m, err := newMigrator(infra)
		if err != nil {
			return process.Fail(stderr, "migrator init failed", err)
		}
		defer func() {
			srcErr, dbErr := m.Close()
			if err := errors.Join(srcErr, dbErr); err != nil {
				_, _ = fmt.Fprintln(stderr, "migrator close:", err)
			}
		}()
		return fn(m)
	}
	return withInfrastructure(stdout, stderr, run)
}
