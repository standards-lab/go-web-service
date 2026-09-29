// Package sdk stages the service's library promotion candidates: request,
// response, and persistence conventions proven here in running code before
// they move outward. The package is flat by design (a staging area meant to
// empty out accumulates no sub-packages) and each file is named for the
// library its contents are bound for. The reads-era tenants landed in
// go-database v0.3.0 and go-web-sdk v0.5.0; the If-Match parse and the
// strict body decode landed in go-web-sdk v0.6.0. web.go now stages two
// tenants for go-web-sdk: the typed path-value parse, the third request
// helper beside IfMatch and DecodeJSON, and the guarded-command read that
// composes the three. reactor.go stages one tenant for go-core: the
// reactor, which joins one source of occurrences to one function for the
// process lifetime, with the Every and Wake sources. It is the
// spike-messaging reactor with Wake and the drain signal (Draining)
// added, planned to move to go-core as its own package. gate.go stages a second tenant for go-core: the quiesce
// gate, a context-aware readers-writer gate that prefers its exclusive
// side. A reactor's work holds it shared, and an operation that must pause
// that work holds it exclusively. The v1.data.evaluation task rules on
// every tenant.
package sdk
