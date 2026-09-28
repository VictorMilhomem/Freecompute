// Package freechains provides a typed, testable Go API wrapping the Freechains v0.20 CLI.
//
// The package follows a ports-and-adapters architecture: Client exposes high-level
// operations over chains, posts, votes, and reps using a Runner interface. ExecRunner
// executes the underlying binary, while SerialRunner ensures serialized execution.
// Daemon manages background daemon processes without blocking.
package freechains
