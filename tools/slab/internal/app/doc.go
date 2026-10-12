// Package app is the composition root. [New] describes slab's dependencies
// as one go-core graph and builds the command tree on go-cli-sdk over it;
// [App.Run] dispatches the process arguments over that tree. The package is
// one file per layer, so its table of contents is slab's layer list:
//
//   - app.go: [App], [New], and [Nodes], which call the layer files'
//     define and mount functions in layer order, lowest first;
//   - infrastructure.go: defineInfrastructure, the lifecycle configuration
//     node, one node per run setting (base, grafana, tempo, repo, and no
//     color), each read in its constructor from its default and its SLAB_*
//     variable with its root flag (--base, --grafana, --tempo, --repo,
//     --no-color) overriding them, and the client node, the HTTP client to
//     the service bound to the base node's URL; the root flags, accepted at
//     any depth; and the output source the families on the tree render
//     through, over the no color node;
//   - domain.go and admin.go: mountDomain, one root.Add(pkg.Commands(...)...)
//     per domain (org and docs), and mountAdmin, the admin command over the
//     database and storage families;
//   - demo.go: the demo command, over slab's own scenario runner, and list,
//     the scenario listing;
//   - version.go: the version command, which declares no nodes.
//
// [Nodes] holds one handle per node: each define function fills its own
// part of one Nodes value. Defining the nodes constructs nothing. A command
// declares the nodes it needs with Use; the dispatcher builds them, with
// the lifecycle configuration node, only when the command runs, and shuts
// them down when it returns. A run that builds nothing, such as help, a
// usage error, version, or list, reads no configuration. The root's PreRun
// records which root flags a dispatch set, so a setting's node applies its
// flag over its variable only when the flag was given. No aggregate
// configuration gathers the settings: a command declares only the ones it
// reads. Every command
// dispatches through go-cli-sdk.
//
// The App publishes its composition: [App.Graph], [App.Nodes], and
// [App.Root] return the graph, a handle on each of its nodes, and the
// command tree. The program itself never calls them; they are for a caller
// that observes what a run builds, Replaces a node's constructor with a
// substitute, or adds a command over the nodes, before the App runs.
//
// The package exports:
//
//   - [App] and [New], which assemble the program and run it
//   - [Nodes], the graph's nodes
package app
