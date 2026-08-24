package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"

	"github.com/standards-lab/go-core/process"
	"github.com/standards-lab/go-database/seed"

	"github.com/standards-lab/go-web-service/internal/infrastructure"
)

func seedCmd(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("seed", flag.ContinueOnError)
	fl.SetOutput(stderr)

	if err := fl.Parse(args); err != nil {
		return process.ExitUsage
	}

	fn := func(ctx context.Context, infra *infrastructure.Infrastructure) int {
		runner, err := seed.New(infra.DB.Conn(), infra.Logger, seed.JSON{})
		if err != nil {
			return process.Fail(stderr, "seed init failed", err)
		}
		if err := runner.Run(ctx, seeds, seedSteps()...); err != nil {
			return process.Fail(stderr, "seed failed", err)
		}

		_, _ = fmt.Fprintln(stdout, "seed: ok")
		return process.ExitOK
	}

	return withInfrastructure(stdout, stderr, fn)
}

func seedSteps() []seed.Step {
	return []seed.Step{
		seed.Table("seeds/organizations.json", loadOrganizations),
	}
}

type organization struct {
	Parent string `json:"parent"`
	Code   string `json:"code"`
	Name   string `json:"name"`
}

func loadOrganizations(ctx context.Context, tx *sql.Tx, rows []organization) error {
	const insert = `
		INSERT INTO organization (parent_id, code, name)
		VALUES ($1, $2, $3)
		ON CONFLICT ON CONSTRAINT uq_organization_parent_code DO NOTHING`
	const find = `
		SELECT id FROM organization
		WHERE parent_id IS NOT DISTINCT FROM $1 AND code = $2`

	ids := make(map[string]string, len(rows))
	for _, row := range rows {
		var parentID any
		if row.Parent != "" {
			id, ok := ids[row.Parent]
			if !ok {
				return fmt.Errorf(
					"organization %s: parent %q not seeded before it",
					row.Code,
					row.Parent,
				)
			}
			parentID = id
		}
		if _, dup := ids[row.Code]; dup {
			return fmt.Errorf(
				"organization %s: code reused within the seed file",
				row.Code,
			)
		}

		if _, err := tx.ExecContext(ctx, insert, parentID, row.Code, row.Name); err != nil {
			return fmt.Errorf("insert organization %s: %w", row.Code, err)
		}

		var id string
		if err := tx.QueryRowContext(ctx, find, parentID, row.Code).Scan(&id); err != nil {
			return fmt.Errorf("find organization %s: %w", row.Code, err)
		}
		ids[row.Code] = id
	}
	return nil
}
