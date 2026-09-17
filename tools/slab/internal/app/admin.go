package app

import (
	"github.com/spf13/cobra"

	"github.com/standards-lab/go-web-service/tools/slab/admin/database"
)

// Admin composes the admin clients, one field per admin domain: the
// administrative counterpart of Domain. Each field is the constructor the
// package's commands call when a subcommand runs, for the reason Domain's
// are.
type Admin struct {
	Database func() *database.Client
}

// newAdmin wires the admin layer over infra, each admin package's client
// constructor closed over the infrastructure's client.
func newAdmin(infra *Infrastructure) *Admin {
	return &Admin{
		Database: func() *database.Client {
			return database.NewClient(infra.Client())
		},
	}
}

// mountAdmin builds the admin mount, the admin command, with each admin
// domain's commands mounted under it. Run without a subcommand, it prints
// its help.
func mountAdmin(adm *Admin) *cobra.Command {
	admin := &cobra.Command{
		Use:   "admin",
		Short: "Call the admin services' endpoints",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	admin.AddCommand(database.Commands(adm.Database))
	return admin
}
