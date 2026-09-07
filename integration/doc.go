// Package integration is the service's integration tier: the harness that
// runs the composed service against the compose stack and drives it through
// its production seams, and, under the integration build tag, the suite that
// asserts the service's behavior through its API.
//
// The harness treats the service as the binary. [Main] builds cmd/server
// once per suite run; [Start] runs it as a subprocess configured by APP_*
// environment variables on a reserved port, waits for its liveness probe,
// and [Service.Stop] signals it and returns its exit code. State control goes
// through the admin mount ([Reset]), fault injection through the network (a
// [Forwarder] between the service and a backing service), so nothing in the
// runtime exists for the tests' sake. The suite files, tagged integration,
// need a running compose stack; the harness itself and its own tests do not,
// so the unit tier type-checks and proves it on every pull request.
package integration
