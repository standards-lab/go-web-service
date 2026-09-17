// Package app is the composition root: [App] assembles the layers into a
// command tree and runs the process. The package is one file per layer, so
// its table of contents is slab's layer list: config.go declares the root's
// persistent flags and the resolved values, infrastructure.go constructs the
// HTTP client, domain.go the domain clients and their commands, admin.go the
// admin clients and the admin mount, demo.go the demo mount and the listing;
// commands.go is the list of mounts. Extending slab means editing a layer
// file's body; the signatures, cmd/slab, and [App.Run] stay untouched.
//
// [New] is the cold start, with no I/O: it builds the root command with its
// persistent flags, the infrastructure over the resolved config, the domain
// and admin layers over the infrastructure, and mounts each layer's commands
// on the root. One thing differs from a server's composition root: the
// values the layers depend on (the base URL above all) are persistent flags,
// and cobra parses those during execution, after the tree is built. So the
// infrastructure constructs the client on demand rather than at New, and each
// layer closes a client constructor over it that a subcommand's RunE calls
// when it runs. Nothing below the root reads a flag before then.
//
// [App.Run] is the hot start: it executes the tree under ctx, renders the
// error a command returns, and returns the process exit code.
package app
