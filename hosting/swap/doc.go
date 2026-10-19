// Package swap swaps the models a llama.cpp router has loaded for one of the profile's sets,
// over the router's own API, and times each step.
//
// A swap first unloads every model the router has loaded, or is loading, that the incoming set
// doesn't hold, one at a time, polling GET /models until each is listed unloaded. Only once
// every unload has finished does it load the incoming set's models that aren't loaded, one at
// a time, polling until each is listed loaded; a model the router hasn't the files of downloads
// through its own Hugging Face fetch as it loads. A model both sets share stays loaded,
// untouched. The swap ends by checking the router lists exactly the incoming set loaded.
// Unloading first keeps the router under its --models-max, which would otherwise evict a model
// of its own choosing to make room.
//
// The API:
//   - Run swaps the router to a Config's models and returns a Report: the models kept, each
//     unload and load a Step with its time, and the total.
//   - Reload unloads each of a Config's models and loads it again, every unload before the first
//     load, leaving every other model as it is: evaluate starts each run from freshly loaded models.
//   - DefaultInterval, DefaultLoadTimeout and DefaultUnloadTimeout are the Config's defaults.
package swap
