package app

import (
	"github.com/standards-lab/go-cli-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/admin/database"
	"github.com/standards-lab/go-web-service/tools/slab/admin/storage"
)

func mountAdmin(root *cli.Command, n *Nodes) {
	out := outputSource(n)
	admin := &cli.Command{
		Name:    "admin",
		Summary: "Call the admin services' endpoints",
	}
	admin.Add(database.Commands(n.Client, out)...)
	admin.Add(storage.Commands(n.Client, out)...)
	root.Add(admin)
}
