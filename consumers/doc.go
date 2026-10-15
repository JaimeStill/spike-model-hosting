// Package consumers sets up the coding agents the spike drives against the router, Pi, Claude
// Code and OpenCode, so every tool and test that runs one runs it the same way: the version the
// consumers mise environment pins, on the chat model of the profile's set the run names, its
// state under a directory of the run's own and never the user's. Any model ID serves: nothing
// here names a model.
//
// Each consumer reaches the router its own way. Pi uses its built-in llama.cpp provider, its
// session's model named in its harness options, at the chat model's thinking level. Claude Code
// sends Anthropic messages to the router, every model alias mapped to the chat model, on the
// subscription login of a configuration directory of the run's own, with no API key or token,
// and no reasoning setting of the spike's. OpenCode uses an OpenAI-compatible provider, with
// the chat model's interleaved setting naming reasoning_content, so a turn that called tools
// sends its reasoning back, and the chat model's options, such as its reasoning effort.
//
// The API:
//   - Provider is the provider Pi's and OpenCode's sessions name.
//   - PiVersion, ClaudeVersion and OpenCodeVersion are the consumers mise environment's pins.
//   - Executable returns a consumer's executable: the one given, or the pinned one mise finds.
//   - Pi, ClaudeCode and OpenCode return a consumer's harness driver on the router, on the chat
//     model given; Pi at the thinking level given, OpenCode with the options given. PiSettings
//     is the settings file Pi reads the thinking level from. ClaudeEnv is the environment that
//     points Claude Code there, CheckClaudeLogin checks its configuration directory holds a
//     login, and InterleavedConfig is the configuration file OpenCode loads for the chat model.
package consumers
