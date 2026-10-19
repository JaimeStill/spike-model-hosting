// Package router reads a running llama.cpp router's state over HTTP (the models it lists, their
// status, and the build it runs), and loads and unloads its models. The spike's tools read the
// state before a run, so a run never starts against a model a request would load, and every
// measurement names its build. Only the swap package loads or unloads a model, and only evaluate's
// sweep has the router read its presets again.
//
// The API:
//   - Models returns each model GET /models lists, a Model with its status and whether its
//     last load failed; Statuses returns their status by ID. StatusLoaded and StatusUnloaded
//     are the status values the package reads.
//   - CheckLoaded fails unless GET /models lists each of the given models as loaded.
//   - Build returns the router's build, from GET /props.
//   - Load and Unload ask the router to load or unload a model, POST /models/load and
//     /models/unload, and return once it has taken the request.
//   - ReloadPresets has the router read its presets file again, GET /models?reload=1, unloading
//     each running model whose section changed; ServedAt returns the slots and context a loaded
//     model's server runs at (Served), from GET /props.
package router
