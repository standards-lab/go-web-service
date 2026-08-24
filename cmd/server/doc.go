// The server binary: the process entrypoint and nothing else. main.go traps
// the termination signals into the root context, loads the configuration,
// hands both to the composition root — internal/app, where the App primitive
// orchestrates the whole assembly — and exits with the code Run returns.
// Extending the service never touches this package.
package main
