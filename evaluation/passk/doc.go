// Package passk runs clutch's conformance suite over the router k times per harness, and judges
// each harness's fit: whether it can drive the model, by one absolute bar every harness is held
// to alike.
//
// clutch's conform package runs a Cell, one harness over one provider, through its nine
// capabilities. This package builds the cells the spike runs from the laptop, its Harnesses,
// each a consumer on the chat model of the profile's set the run names (Config.Roles) over the
// router, on the consumers package's driver, its state under the run's own directory and never the user's:
//   - Pi on its llama.cpp provider, which the driver's bridge constrains to tool_choice
//     "required" on every request that offers respond, at the chat model's thinking level
//     (consumers.Pi).
//   - OpenCode on an OpenAI-compatible provider, with the chat model's interleaved setting naming
//     reasoning_content, so a turn that called tools sends its reasoning back, and its options;
//     its tool_choice stays auto (consumers.OpenCode).
//   - Claude Code over the router's Anthropic messages, on the subscription login of its own
//     configuration directory, every model alias mapped to the chat model (consumers.ClaudeCode).
//
// The fit bar: a harness fits at k when every applicable capability passed in each of its k
// trials and the router answered none of its model requests with an error. A capability the
// harness reported n/a in every trial, one it can't exercise (Claude Code's dropped-image), is
// excluded from its bar and noted; skip is a failure, since the capability never ran. The first
// trial that fails the bar decides: the harness can't fit whatever trials still to come do, so
// a run may drop it there.
//
// The API:
//   - Pi, OpenCode and Claude return a consumer's cell from a Config, whose Roles name the
//     set's chat, vision, audio and embedding models and whose Reasoning is the chat model's
//     consumer reasoning setting (profile.ConsumerReasoning). ClaudeDriver returns the Claude
//     cell's driver, and SessionOptions the options a cell's sessions open with.
//   - Version reads the version a consumer's executable reports, which a cell checks against
//     the consumers package's pin.
//   - Harness is a harness by name and how its cell is built; Harnesses lists the pinned ones in
//     the order a run takes them, ConsumerNames their names, and LookupHarness finds one by
//     name. A harness joins with one entry.
//   - Tally counts a consumer's k trials, each a Trial, into a Row: a Cell per capability with
//     its passes, failures, n/a, skips and reasons, and its invalid requests.
//   - Judge judges a harness's trials against the fit bar at k into a Fit: whether it fits, the
//     capabilities it excluded, and the first trial that failed with each failed cell's reason
//     and each invalid request. Fit.Verdict says so in a line; Fit.Applicable counts the
//     capabilities its bar holds, and Fit.Failed says whether a trial failed it.
//   - Invalid picks out of a recording (wire.Recorder) the model requests the router answered
//     with an error.
//   - Runner runs a Plan of k trials over the router, every harness's cell once a trial, each
//     through a wire.Recorder of its own whose recording it saves, and returns the Result: the
//     trials, Rows and Fits, the wall time and each harness's share of it. With Plan.Drop a
//     harness drops at its first failed trial, and the run ends once every harness has dropped
//     or run all k.
//   - WriteTable writes Fits as a Markdown table, a column a capability, WriteFits each
//     harness's verdict, and WriteReasons why each trial that didn't pass didn't, and each
//     invalid request.
package passk
