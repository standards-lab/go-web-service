// Package app is the composition root. [New] describes slab's dependencies
// as one go-core graph and builds the command tree on go-cli-sdk over it;
// [App.Run] dispatches the process arguments over that tree. The package is
// one file per layer, so its table of contents is slab's layer list:
//
//   - app.go: [App], [New], and [Nodes], which call the layer files'
//     define and mount functions in layer order, lowest first;
//   - config.go: [Config], slab's run configuration, read from SLAB_*
//     variables with the root flags (--base, --grafana, --tempo, --repo,
//     --no-color) overriding them, and those flags, accepted at any depth;
//   - infrastructure.go: defineInfrastructure, the lifecycle and run
//     configuration nodes and the client node, the HTTP client to the
//     service bound to the config's base URL; and the output source the
//     families on the tree render through;
//   - domain.go, admin.go, and demo.go: the domain commands on the tree
//     (org and docs), the admin command on the tree over the database and
//     storage families, and the demo mount and the scenario listing;
//   - version.go: the version command, which declares no nodes;
//   - legacy.go and commands.go: the temporary bridge to the command
//     families still on cobra, and their list of mounts.
//
// [Nodes] holds one handle per node: each define function fills its own
// part of one Nodes value. Defining the nodes constructs nothing. A command
// declares the nodes it needs with Use; the dispatcher builds them, with
// the lifecycle configuration node, only when the command runs, and shuts
// them down when it returns. A run that builds nothing, such as help, a
// usage error, or version, reads no configuration. The root's PreRun
// records which root flags a dispatch set, so the config node applies a
// flag over its variable only when the flag was given.
//
// The org, docs, and admin families are on the tree. The command families
// demo and list are moving onto it one by one. Until each moves,
// [App.Run] hands a run that selects it to a cobra tree built per run as
// before the move, with its own flags, words, requests, and exit codes;
// the root's help lists them beside the commands on the tree.
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
//   - [Config], [ConfigEnv], and [NewConfigEnv], the run configuration and
//     the variables it reads
package app
