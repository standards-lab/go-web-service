// Package sdk stages the service's library promotion candidates: request,
// response, and persistence conventions proven here in running code before
// they move outward. The package is flat by design — a staging area meant to
// empty out accumulates no sub-packages — and each file is named for the
// library its contents are bound for. The reads-era tenants promoted and
// landed in go-database v0.3.0 and go-web-sdk v0.5.0; web.go now stages the
// If-Match precondition parse for go-web-sdk, where its natural landing
// joins ErrorWriter's built-in vocabulary. The v1.data.evaluation task
// rules on every tenant.
package sdk
