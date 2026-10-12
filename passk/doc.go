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
//   - Pi, OpenCode and Claude return a consumer's cell from a Config.
//   - Tally counts a consumer's k trials into a Row: each capability's passes, failures and
//     reasons, its pass^k, and its invalid requests; Row.MeetsBar applies the bar.
//   - Invalid picks out of a recording (wire.Recorder) the model requests the router answered
//     with an error.
package passk
