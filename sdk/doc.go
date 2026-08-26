// Package sdk stages the service's library promotion candidates: request,
// response, and persistence conventions proven here in running code before
// they move outward. The package is flat by design — a staging area meant to
// empty out accumulates no sub-packages — and each file is named for the
// library its contents are bound for: web.go for go-web-sdk, database.go
// for go-database. The v1.data.evaluation task rules on every tenant.
package sdk
