// Command client is FlowCore's reference client: a working application built on
// the library, meant to be run, read, lifted from, or forked as the starting
// point for a real one.
//
// It exists because a boundary is invisible from one side. FlowCore's central
// claims — that references are opaque, that the library never calls a model,
// that the caller owns dispatch, that subjects live elsewhere — cannot be read
// from the API alone. Each becomes legible only when something is shown doing
// the other half, and this is that something.
//
// Run it with `go run .` from this directory. See README.md.
package main

func main() {}
