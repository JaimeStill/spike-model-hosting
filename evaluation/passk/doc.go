// Package passk runs clutch's conformance suite over the router k times per consumer, and
// judges each consumer by pass^k against the bar for a gpt-oss implementation, or against a
// comparison row: a model's own pass^k on a build, cell by cell.
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
//   - Runner runs a Plan of k trials over the router, every consumer's cell once a trial, each
//     through a wire.Recorder of its own whose recording it saves, and returns the Result: the
//     trials and Rows. A Plan's Stop may end the run once every consumer has run its first trial;
//     ConsumerNames are the consumers, in the order they run.
//   - Comparison is a comparison row, which LoadComparison reads from a TOML file
//     (evaluation/comparison.toml, gpt-oss-120b's on b11669): each consumer's cells and invalid
//     requests, and whether it is gated. Comparison.Judge names where a Row falls short of its
//     consumer's row, and Comparison.Stop, a Plan's Stop, ends a run once a gated consumer can't
//     reach its row whatever its trials still to run do.
//   - WriteTable writes Rows as a Markdown table, a column a capability, and WriteReasons why each
//     trial that didn't pass didn't, and each invalid request.
package passk
