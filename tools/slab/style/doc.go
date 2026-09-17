// Package style is the one place slab's ANSI terminal styling lives.
// ColorEnabled decides whether a run should emit color at all; New builds a
// Style carrying that decision, and every method on it returns its input
// unchanged when color is off, so the same code path serves a pipe and a
// terminal.
//
// style.go holds the styling core: the semantic wrappers (Bold, Heading,
// Key, Value, ...) every format builds on. A format's own coloring lives in
// its own file, named for what it colors (sql.go, json.go): it composes the
// core wrappers into one method on Style, named for the format (SQL, JSON),
// and reaches for nothing outside the core and its own file.
package style
