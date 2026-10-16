// Package passk runs clutch's conformance suite over the router k times per consumer, and
// judges each consumer by pass^k against the bar for a gpt-oss implementation.
//
// clutch's conform package runs a Cell, one harness over one provider, through its nine
// capabilities. This package builds the cells the spike runs from the laptop, each a consumer on
// the chat model of the profile's set the run names (Config.Roles) over the router, on the
// consumers package's driver, its state under the run's own directory and never the user's:
//   - Pi on its llama.cpp provider, which the driver's bridge constrains to tool_choice
//     "required" on every request that offers respond, at the chat model's thinking level
//     (consumers.Pi).
//   - OpenCode on an OpenAI-compatible provider, with the chat model's interleaved setting naming
//     reasoning_content, so a turn that called tools sends its reasoning back, and its options;
//     its tool_choice stays auto (consumers.OpenCode).
//   - Claude Code over the router's Anthropic messages, on the subscription login of its own
//     configuration directory, every model alias mapped to the chat model (consumers.ClaudeCode).
//
// A capability is a task, and a consumer's task passes at k when every one of its k trials
// passed; n/a and skip are not passes, so a capability a consumer can't run counts against it.
// pass^k is the share of the nine tasks that passed at k. A consumer meets the bar when the
// router answered none of its model requests with an error and pass^k is at least 90%, which
// over nine tasks means all nine.
//
// The API:
//   - Pi, OpenCode and Claude return a consumer's cell from a Config, whose Roles name the
//     set's chat, vision, audio and embedding models and whose Reasoning is the chat model's
//     consumer reasoning setting (profile.ConsumerReasoning). ClaudeDriver returns the Claude
//     cell's driver, and SessionOptions the options a cell's sessions open with.
//   - Version reads the version a consumer's executable reports, which a cell checks against
//     the consumers package's pin.
//   - Tally counts a consumer's k trials, each a Trial, into a Row: a Cell per capability with
//     its passes, failures and reasons, the Row's pass^k (Row.Passed, Row.PassK), and its
//     invalid requests; Row.MeetsBar applies Bar.
//   - Invalid picks out of a recording (wire.Recorder) the model requests the router answered
//     with an error.
package passk
