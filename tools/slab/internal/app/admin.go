package app

import (
	"github.com/standards-lab/go-cli-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/admin/database"
	"github.com/standards-lab/go-web-service/tools/slab/admin/storage"
)

// mountAdmin mounts the admin command at root, with each admin package's
// commands added under it, one admin.Add(pkg.Commands(...)...) per
// package, each handed the client node from n and the output source to
// render through. The admin endpoints share the service's base URL with
// the domain ones, so the families send through the same client node. The
// admin command declares no node itself; each family declares what its
// leaves need.
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
