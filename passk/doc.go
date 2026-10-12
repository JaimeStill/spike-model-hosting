// Package passk runs clutch's conformance suite over the router k times per consumer, and
// judges each consumer by pass^k against the bar for a gpt-oss implementation.
//
// clutch's conform package runs a Cell, one harness over one provider, through its nine
// capabilities. This package builds the cells the spike runs from the laptop, each a consumer on
// gpt-oss-120b over the router, its state under the run's own directory and never the user's:
//   - Pi on its llama.cpp provider, which the driver's bridge constrains to tool_choice
//     "required" on every request that offers respond.
//   - OpenCode on an OpenAI-compatible provider, with gpt-oss-120b's interleaved setting naming
//     reasoning_content, so a turn that called tools sends its reasoning back; its tool_choice
//     stays auto.
//   - Claude Code over the router's Anthropic messages, on the subscription login of its own
//     configuration directory, every model alias mapped to gpt-oss-120b (concurrent.ClaudeCode).
//
// A capability is a task, and a consumer's task passes at k when every one of its k trials
// passed; n/a and skip are not passes, so a capability a consumer can't run counts against it.
// pass^k is the share of the nine tasks that passed at k. A consumer meets the bar when the
// router answered none of its model requests with an error and pass^k is at least 90%, which
// over nine tasks means all nine.
//
// The API:
//   - Pi, OpenCode and Claude return a consumer's cell from a Config; PiDriver, OpenCodeDriver
//     and ClaudeDriver its driver, SessionOptions the options its sessions open with, and
//     InterleavedConfig the OpenCode configuration file the OpenCode driver loads.
//   - ChatModel, VisionModel, AudioModel and EmbedModel are the router's models the cells run
//     on, and Models lists them; Provider is the provider Pi's and OpenCode's sessions name.
//   - PiVersion, ClaudeVersion and OpenCodeVersion are the consumers mise environment's pins,
//     and Version reads the version a consumer's executable reports.
//   - Tally counts a consumer's k trials, each a Trial, into a Row: a Cell per capability with
//     its passes, failures and reasons, the Row's pass^k (Row.Passed, Row.PassK), and its
//     invalid requests; Row.MeetsBar applies Bar.
//   - Invalid picks out of a recording (wire.Recorder) the model requests the router answered
//     with an error.
package passk
