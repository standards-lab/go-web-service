// Package style is the one place slab's ANSI terminal styling lives.
//
// The package exports:
//
//   - [ColorEnabled], which decides whether a run emits color at all
//   - [Style] and [New], the styling that carries that decision
//
// style.go holds the styling core: the semantic wrappers (Bold, Heading,
// Key, Value, ...) every format builds on. A format's own coloring lives in
// its own file, named for what it colors (sql.go, json.go): it composes the
// core wrappers into one method on Style, named for the format (SQL, JSON),
// and reaches for nothing outside the core and its own file.
package style
