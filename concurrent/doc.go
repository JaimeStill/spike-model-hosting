// Package concurrent runs several consumers' streams against the router at once, and checks
// that no answer reached the wrong request.
//
// A run has chat streams and embedding streams. Each chat stream is one consumer kind, such as
// Pi or the model client, with a number of workers; the workers of every chat stream together
// are sized to the chat model's slots, so each slot carries a request for the whole run. Every
// worker sends Config.Rounds requests back to back, each carrying a nonce unique to the run, and
// a chat request passes only when its answer carries its own nonce and no other. An embedding
// stream first embeds each of its inputs alone, then embeds them together once per round while
// the chat streams run; each vector must match its own input's lone embedding within
// EmbedTolerance.
//
// The API:
//   - Run runs a Config and returns its Report; Report.Passed and Report.Failures say whether
//     the run passed and, when it didn't, which requests failed and why.
//   - ChatStream and EmbedStream are a run's streams; Chatter and Embedder are the consumers
//     they drive.
//   - ModelChat and ModelEmbed drive the model client (spike-harness-driver's model package).
//   - HarnessChat drives any harness.Driver, such as Pi's, one session per request; a further
//     coding agent joins a run as another HarnessChat stream.
//   - Cosine is the similarity the embedding check compares by.
package concurrent
