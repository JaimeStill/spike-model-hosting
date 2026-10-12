// Package wire records what a consumer sends to the router and what comes back, so a finding
// about a consumer's requests rests on the bytes on the wire, not on its documentation.
//
// A Recorder is a reverse proxy that runs between a consumer and the router: the consumer's
// base URL points at the Recorder, which forwards each request and keeps the exchange, its
// request body and its response body, streamed responses included, which pass through as they
// arrive. Credentials never reach a record, and never reach the router: the Recorder drops
// the Authorization, X-Api-Key, Proxy-Authorization and Cookie headers from the forwarded
// request, and records only that each was sent. A consumer signed in to a subscription, such as
// Claude Code, sends that login's bearer token to its base URL, and the router needs none.
//
// The API:
//   - New returns a Recorder for a target; it is an http.Handler, served on a local listener.
//   - Recorder.Exchanges returns the exchanges so far, and Recorder.WriteJSONL writes them, one
//     JSON object a line.
//   - Events splits a recorded server-sent event stream into its events.
package wire
