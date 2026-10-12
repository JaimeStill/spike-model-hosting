// Package router reads a running llama.cpp router's state over HTTP, read-only: which models it
// lists and their status, and the build it runs. The spike's tools read it before a run, so a
// run never starts against a model a request would load, and every measurement names its build.
//
// The API:
//   - Statuses returns each model GET /models lists, with its status.
//   - CheckLoaded fails unless GET /models lists each of the given models as loaded.
//   - Build returns the router's build, from GET /props.
package router
