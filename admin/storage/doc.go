// Package storage is the object storage admin domain: the HTTP half of the
// object store's administration, a route group under the admin mount over
// go-storage's Store, which is its own admin service: the operations are the
// store's own methods, and the composition root starts the store at its
// infrastructure stage, before the schema service seeds objects into it.
//
// The group reads the store's diagnostics, whether it is ready, the
// container it is configured for, and the provider's key rule, and creates
// the container on demand, the operator's correction when the container
// was removed out from under a running service. The composition root
// mounts the group into the admin mount beside admin/database.
package storage
