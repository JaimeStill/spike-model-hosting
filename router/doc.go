// Package router reads a running llama.cpp router's state over HTTP, and loads and unloads its
// models: which models it lists and their status, and the build it runs. The spike's tools read
// it before a run, so a run never starts against a model a request would load, and every
// measurement names its build. Only the swap command loads or unloads a model.
//
// The API:
//   - Models returns each model GET /models lists, a Model with its status and whether its
//     last load failed; Statuses returns their status by ID. StatusLoaded and StatusUnloaded
//     are the status values the package reads.
//   - CheckLoaded fails unless GET /models lists each of the given models as loaded.
//   - Build returns the router's build, from GET /props.
//   - Load and Unload ask the router to load or unload a model, POST /models/load and
//     /models/unload, and return once it has taken the request.
package router
