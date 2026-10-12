package app

import (
	"github.com/standards-lab/go-cli-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/admin/database"
	"github.com/standards-lab/go-web-service/tools/slab/admin/storage"
)

// adminCommand builds the admin layer's command on the tree: the admin
// command, with each admin package's commands mounted under it, each handed
// the client node from n and the output source to render through. The
// admin endpoints share the service's base URL with the domain ones, so the
// families send through the same client node. The admin command declares no
// node itself; each family declares what its leaves need.
func adminCommand(n *Nodes) *cli.Command {
	out := outputSource(n)
	admin := &cli.Command{
		Name:    "admin",
		Summary: "Call the admin services' endpoints",
	}
	return admin.Add(
		database.Commands(n.Client, out),
		storage.Commands(n.Client, out),
	)
}
