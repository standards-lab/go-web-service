// Package integration is the service's integration tier: the harness that
// runs the composed service against the compose stack and drives it through
// its production seams, and, under the integration build tag, the suite that
// asserts the service's behavior through its API.
//
// The harness is the toolkit the SDKs ship beside what it exercises, with
// the service's own configuration and state control on top. go-core's
// processtest builds cmd/server once per suite run ([Main]), runs it as a
// [Service], a subprocess configured by APP_* environment variables on a
// reserved port ([Start], [Launch], shaped by [Options]), reads its exit
// code ([Service.Stop]), and relays the database or the object store
// through a loopback forwarder, at [DatabaseAddr] or [StorageAddr], that a
// test severs to prove the outage path and the sweep's refusals.
// go-web-sdk's webtest drives the service through its HTTP surface and
// observes its liveness probe ([Service.Ready]). [Objects] reads the object
// store beneath the API, so a test can assert an object is gone. A failing
// test logs each process's output, so a 500 comes with the service's own
// record of it.
//
// State control goes through the admin mount, so nothing in the runtime
// exists for the tests' sake: [Reset] to a state such as [Default],
// [States], [Revert], [Schema], and [Seed], with their results
// [Transition], [SchemaStatus], [SetStatus], [Migration], and [Seeded].
// [AppSet] names the service's own migration set.
//
// The suite files, tagged integration, need a running compose stack. The
// harness and its own test do not, so the unit tier type-checks and proves
// the harness on every pull request.
package integration
