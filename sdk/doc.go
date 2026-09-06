// Package sdk stages the service's library promotion candidates: request,
// response, and persistence conventions proven here in running code before
// they move outward. The package is flat by design (a staging area meant to
// empty out accumulates no sub-packages) and each file is named for the
// library its contents are bound for. The reads-era tenants landed in
// go-database v0.3.0 and go-web-sdk v0.5.0; the If-Match parse and the
// strict body decode landed in go-web-sdk v0.6.0. web.go now stages the
// typed path-value parse for go-web-sdk, the third request helper beside
// IfMatch and DecodeJSON. The v1.data.evaluation task rules on every tenant.
package sdk
