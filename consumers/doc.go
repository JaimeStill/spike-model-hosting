// Package consumers sets up the coding agents the spike drives against the router, Pi, Claude
// Code and OpenCode, so every tool and test that runs one runs it the same way: the version the
// consumers mise environment pins, on gpt-oss-120b, its state under a directory of the run's own
// and never the user's.
//
// Each consumer reaches the router its own way. Pi uses its built-in llama.cpp provider. Claude
// Code sends Anthropic messages to the router, every model alias mapped to gpt-oss-120b, on the
// subscription login of a configuration directory of the run's own, with no API key or token.
// OpenCode uses an OpenAI-compatible provider, with gpt-oss-120b's interleaved setting naming
// reasoning_content, so a turn that called tools sends its reasoning back.
//
// The API:
//   - ChatModel is gpt-oss-120b's router ID, every consumer's session model; Provider is the
//     provider Pi's and OpenCode's sessions name.
//   - PiVersion, ClaudeVersion and OpenCodeVersion are the consumers mise environment's pins.
//   - Executable returns a consumer's executable: the one given, or the pinned one mise finds.
//   - Pi, ClaudeCode and OpenCode return a consumer's harness driver on the router. ClaudeEnv is
//     the environment that points Claude Code there, CheckClaudeLogin checks its configuration
//     directory holds a login, and InterleavedConfig is the configuration file OpenCode loads.
package consumers
