// Package integration is the service's integration tier: the harness that
// runs the composed service against the compose stack and drives it through
// its production seams, and, under the integration build tag, the suite that
// asserts the service's behavior through its API.
//
// The harness is the toolkit the SDKs ship beside what it exercises, with
// the service's own configuration and state control on top. go-core's
// processtest builds cmd/server once per suite run ([Main]), runs it as a
// subprocess configured by APP_* environment variables on a reserved port
// ([Start], [Launch]), reads its exit code ([Service.Stop]), and relays the
// database through a loopback forwarder a test severs to prove the outage
// path; go-web-sdk's webtest drives it through its HTTP surface and observes
// its liveness probe ([Service.Ready]). State control goes through the admin
// mount ([Reset], [States], [Revert], [Schema], [Seed]), so nothing in the
// runtime exists for the tests' sake. The suite files, tagged integration, need a
// running compose stack; the harness itself and its own test do not, so the
// unit tier type-checks and proves it on every pull request.
package integration
