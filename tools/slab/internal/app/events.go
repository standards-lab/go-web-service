package app

import (
	"github.com/standards-lab/go-cli-sdk"

	"github.com/standards-lab/go-web-service/tools/slab/events"
)

func mountEvents(root *cli.Command, n *Nodes) {
	root.Add(events.Commands(n.Broker, outputSource(n))...)
}
