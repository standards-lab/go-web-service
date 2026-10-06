// Package statement reads a compiled sqlate statement set the way a
// scenario displays it: one statement by name, and its parameters as the
// placeholders a dialect renders them to.
//
// The package exports:
//
//   - [Find], which returns one statement by name
//   - [Placeholders], which pairs each parameter with its placeholder
package statement
