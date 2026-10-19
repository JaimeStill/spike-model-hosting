// Package coldload times a router restart from the outside: how long until every model it served
// is loaded again.
//
// A Tracker follows the restart through polls of the router's GET /models. It waits for the models
// it is given, such as the startup set's, or else for every model the router lists, and first needs
// a poll that lists every one of them loaded; a model outside them is ignored. The restart begins
// at the first poll after that which doesn't see them all loaded, whether the router refused the
// connection, answered with an error, or listed a model not loaded. The Tracker then records when
// the router answered again after refusing, and when each model was listed as loaded again after it
// was gone (the router down, or the model listed not loaded), so a model a stopping router still
// lists counts for nothing. Every time is measured from the restart's start, so each is late by up
// to one poll interval; the last poll that saw them all loaded before the restart bounds the start
// from below.
//
// The API:
//   - Watch polls a router until a restart has every model it waits for loaded again, and
//     writes the times; Follow does the same and returns the Tracker, for a caller that keeps
//     the times as figures (Tracker.Times, Times).
//   - Tracker follows one restart, and its zero value is ready to use; Tracker.Models names the
//     models it waits for. Tracker.Observe takes each poll, Tracker.Ready and Tracker.Done say
//     how far it got, Tracker.Progress describes a restart that didn't finish, and Tracker.Write
//     writes the times.
package coldload
