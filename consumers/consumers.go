package consumers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/JaimeStill/spike-harness-driver/claude"
	"github.com/JaimeStill/spike-harness-driver/opencode"
	"github.com/JaimeStill/spike-harness-driver/pi"
)

// Provider is Pi's built-in provider for a llama.cpp server, which speaks OpenAI chat to
// LLAMA_BASE_URL; OpenCode's driver writes its OpenAI-compatible provider under the same name.
const Provider = "llama.cpp"

// The consumer versions the consumers mise environment (mise.consumers.toml) pins.
const (
	PiVersion       = "1.3.0"
	ClaudeVersion   = "2.1.301"
	OpenCodeVersion = "1.19.3"
)

// claudeLogin is the file Claude Code keeps its subscription login in, in its configuration
// directory, on Linux.
const claudeLogin = ".credentials.json"

// Executable returns the consumer name's executable: given, or else the one the consumers mise
// environment pins (MISE_ENV=consumers mise which name), rather than whatever the user's PATH
// finds first. The error names -name and SPIKE_NAME, the flag and variable the spike's tools
// take the executable from.
func Executable(ctx context.Context, given, name string) (string, error) {
	if given != "" {
		return given, nil
	}
	cmd := exec.CommandContext(ctx, "mise", "which", name)
	cmd.Env = append(os.Environ(), "MISE_ENV=consumers")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("finding the pinned %s (MISE_ENV=consumers mise which %s): %w; set -%s or SPIKE_%s",
			name, name, err, name, strings.ToUpper(name))
	}
	return strings.TrimSpace(string(out)), nil
}

// Pi returns Pi's driver: command on its llama.cpp provider (Provider) pointed at router, its
// agent directory, sessions and cache under dir, never the user's ~/.pi. Pi runs code from its
// agent directory and cache, so the driver refuses dir unless it exists and is the user's alone;
// the caller makes it. The driver's bridge sets tool_choice "required" on every request that
// offers respond.
//
// thinking is Pi's thinking level for the chat model, or empty for Pi's own. Pi reads it from
// its agent directory's settings file (PiSettings), written before the driver is returned, the
// directory created private to the user, replacing any file a run before left. It fails when
// the directory or the file can't be written.
func Pi(command, router, dir, chat, thinking string) (pi.Driver, error) {
	agent := filepath.Join(dir, "agent")
	if err := os.MkdirAll(agent, 0o700); err != nil {
		return pi.Driver{}, err
	}
	if err := os.WriteFile(filepath.Join(agent, "settings.json"), PiSettings(chat, thinking), 0o600); err != nil {
		return pi.Driver{}, err
	}
	return pi.Driver{
		Command:    command,
		AgentDir:   agent,
		SessionDir: filepath.Join(dir, "sessions"),
		CacheDir:   filepath.Join(dir, "cache"),
		// Pi's llama.cpp provider takes the router's root.
		Env: []string{"LLAMA_BASE_URL=" + router},
		// Pi starts from an empty catalog in its own agent directory and lists the router's
		// models once its background refresh lands; sessions starting at once share the router's
		// answer time.
		CatalogWait: 30 * time.Second,
	}, nil
}

// PiSettings is the settings file Pi reads from its agent directory: with thinking set, the
// chat model's thinking level, under modelThinkingLevels by Provider/chat, which Pi applies when
// a session selects the model; with none, no setting at all. Pi clamps the level to the levels
// it reads the model as supporting. Its llama.cpp provider reads a model as reasoning only when
// the template the router reports reads enable_thinking, offers it off and medium, and sends
// chat_template_kwargs enable_thinking true at medium and false at off; it sends no reasoning
// field for any other model, whatever the level. With no setting, a session's level is off.
func PiSettings(chat, thinking string) []byte {
	settings := map[string]any{}
	if thinking != "" {
		settings["modelThinkingLevels"] = map[string]string{Provider + "/" + chat: thinking}
	}
	b, _ := json.Marshal(settings)
	return b
}

// ClaudeEnv is the environment that sends Claude Code's model requests to router, an
// Anthropic Messages endpoint at its root (/v1/messages), as model: ANTHROPIC_MODEL for the
// session's main model, and the opus, sonnet and haiku aliases, which Claude Code also uses for
// its background requests, all mapped to model. It sets no ANTHROPIC_API_KEY or
// ANTHROPIC_AUTH_TOKEN, so Claude Code keeps the subscription login of its configuration
// directory and sends that login's bearer token to router, which ignores it.
func ClaudeEnv(router, model string) []string {
	return []string{
		"ANTHROPIC_BASE_URL=" + router,
		"ANTHROPIC_MODEL=" + model,
		"ANTHROPIC_DEFAULT_OPUS_MODEL=" + model,
		"ANTHROPIC_DEFAULT_SONNET_MODEL=" + model,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL=" + model,
	}
}

// ClaudeCode returns Claude Code's driver: command, from configDir, a configuration directory
// of the run's own that holds the subscription login (CheckClaudeLogin), its model requests
// sent to router as model (ClaudeEnv). A session opens on Provider claude.Provider, or none.
func ClaudeCode(command, configDir, router, model string) claude.Driver {
	return claude.Driver{Command: command, ConfigDir: configDir, Env: ClaudeEnv(router, model)}
}

// CheckClaudeLogin fails, naming the command that signs in, unless configDir holds a Claude
// Code subscription login. The login is made once with CLAUDE_CONFIG_DIR set to configDir, then
// /login.
func CheckClaudeLogin(configDir string) error {
	if _, err := os.Stat(filepath.Join(configDir, claudeLogin)); err != nil {
		return fmt.Errorf("%w: sign in once with `CLAUDE_CONFIG_DIR=%s MISE_ENV=consumers mise exec -- claude`, then /login", err, configDir)
	}
	return nil
}

// OpenCode returns OpenCode's driver: command on an OpenAI-compatible provider named Provider at
// router/v1, its configuration, data and sessions under dir, with the chat model's interleaved
// setting naming reasoning_content and its options (InterleavedConfig). The provider lists each of
// imageModels as taking images, and chat, unless it is one of them, as text only, which the driver
// does on its own. The driver writes its own configuration into OPENCODE_CONFIG_CONTENT; the
// interleaved setting goes in the explicit configuration file OPENCODE_CONFIG names,
// dir/interleaved.json, which OpenCode merges beneath it, model by model. It fails when dir or that
// file can't be written.
func OpenCode(command, router, dir, chat string, options map[string]any, imageModels ...string) (opencode.Driver, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return opencode.Driver{}, err
	}
	interleaved := filepath.Join(dir, "interleaved.json")
	if err := os.WriteFile(interleaved, InterleavedConfig(chat, options), 0o600); err != nil {
		return opencode.Driver{}, err
	}
	models := map[string]opencode.Model{}
	for _, m := range imageModels {
		models[m] = opencode.Model{Image: true}
	}
	return opencode.Driver{
		Command:  command,
		StateDir: filepath.Join(dir, "state"),
		CacheDir: filepath.Join(dir, "cache"),
		Providers: map[string]opencode.Provider{Provider: {
			BaseURL: strings.TrimSuffix(router, "/") + "/v1",
			Models:  models,
		}},
		Env: []string{"OPENCODE_CONFIG=" + interleaved},
	}, nil
}

// InterleavedConfig is the OpenCode configuration that sets the chat model's interleaved field
// to reasoning_content: OpenCode then moves each assistant message's reasoning into that field
// of the request, which llama.cpp reads back into the model's prompt through its chat template.
// options, when there are any, are the model's options, which OpenCode sends with each of its
// requests: an AI SDK option, such as reasoningEffort, as its request field, reasoning_effort,
// and any other as given, such as chat_template_kwargs. They are how a chat model's reasoning
// setting reaches the router.
func InterleavedConfig(chat string, options map[string]any) []byte {
	model := map[string]any{"interleaved": map[string]string{"field": "reasoning_content"}}
	if len(options) > 0 {
		model["options"] = options
	}
	b, _ := json.Marshal(map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"provider": map[string]any{Provider: map[string]any{
			"models": map[string]any{chat: model},
		}},
	})
	return b
}
