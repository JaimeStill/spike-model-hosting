package consumers_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/JaimeStill/spike-model-hosting/consumers"
)

// router is the router's root behind a trailing slash, as a user might give it.
const router = "http://127.0.0.1:9999/"

// TestPi shows Pi runs the given executable on its llama.cpp provider at the router, its agent
// directory, sessions and cache all under the given directory, never ~/.pi.
func TestPi(t *testing.T) {
	dir := t.TempDir()
	d := consumers.Pi("/bin/pi", router, dir)
	if d.Command != "/bin/pi" {
		t.Errorf("Command = %q, want /bin/pi", d.Command)
	}
	for name, got := range map[string]string{"AgentDir": d.AgentDir, "SessionDir": d.SessionDir, "CacheDir": d.CacheDir} {
		if filepath.Dir(got) != dir {
			t.Errorf("%s = %q, want a directory in %s", name, got, dir)
		}
	}
	if want := []string{"LLAMA_BASE_URL=" + router}; !slices.Equal(d.Env, want) {
		t.Errorf("Env = %q, want %q", d.Env, want)
	}
}

// TestClaudeCode shows Claude Code's driver runs the given executable from the run's own
// configuration directory, sends every model request to the router as the one model, whichever
// alias asks, and carries nothing else: no API key or token, which would take the place of the
// subscription login.
func TestClaudeCode(t *testing.T) {
	d := consumers.ClaudeCode("/bin/claude", "/repo/.state/claude", router, consumers.ChatModel)
	if d.Command != "/bin/claude" || d.ConfigDir != "/repo/.state/claude" {
		t.Errorf("Command %q, ConfigDir %q; want the executable and the configuration directory given", d.Command, d.ConfigDir)
	}
	want := []string{
		"ANTHROPIC_BASE_URL=" + router,
		"ANTHROPIC_MODEL=" + consumers.ChatModel,
		"ANTHROPIC_DEFAULT_OPUS_MODEL=" + consumers.ChatModel,
		"ANTHROPIC_DEFAULT_SONNET_MODEL=" + consumers.ChatModel,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL=" + consumers.ChatModel,
	}
	if !slices.Equal(d.Env, want) {
		t.Errorf("Env = %q, want %q", d.Env, want)
	}
	if !slices.Equal(consumers.ClaudeEnv(router, consumers.ChatModel), want) {
		t.Errorf("ClaudeEnv = %q, want %q", consumers.ClaudeEnv(router, consumers.ChatModel), want)
	}
	for _, v := range d.Env {
		if strings.HasPrefix(v, "ANTHROPIC_API_KEY=") || strings.HasPrefix(v, "ANTHROPIC_AUTH_TOKEN=") {
			t.Errorf("Env carries %q, which replaces the subscription login", v)
		}
	}
}

// TestCheckClaudeLogin shows a configuration directory without a login fails, naming the
// command that signs in there, and one with a login passes.
func TestCheckClaudeLogin(t *testing.T) {
	dir := t.TempDir()
	err := consumers.CheckClaudeLogin(dir)
	want := "CLAUDE_CONFIG_DIR=" + dir + " MISE_ENV=consumers mise exec -- claude"
	if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "/login") {
		t.Errorf("CheckClaudeLogin = %v, want an error naming %q and /login", err, want)
	}
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := consumers.CheckClaudeLogin(dir); err != nil {
		t.Errorf("CheckClaudeLogin with a login = %v, want nil", err)
	}
}

// TestOpenCode shows OpenCode talks to the router's OpenAI-compatible base, lists the image
// models given as taking images, keeps its state under the given directory, and loads the
// configuration file that sets gpt-oss-120b's interleaved field to reasoning_content.
func TestOpenCode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "opencode")
	const vision = "ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0"
	d, err := consumers.OpenCode("/bin/opencode", router, dir, vision)
	if err != nil {
		t.Fatal(err)
	}
	if d.Command != "/bin/opencode" {
		t.Errorf("Command = %q, want /bin/opencode", d.Command)
	}
	p, ok := d.Providers[consumers.Provider]
	if !ok || len(d.Providers) != 1 {
		t.Fatalf("Providers = %v, want only %q", d.Providers, consumers.Provider)
	}
	if p.BaseURL != "http://127.0.0.1:9999/v1" {
		t.Errorf("BaseURL = %q, want the router's /v1", p.BaseURL)
	}
	if m, ok := p.Models[vision]; !ok || !m.Image || len(p.Models) != 1 {
		t.Errorf("Models = %v, want only %s, taking images", p.Models, vision)
	}
	if !strings.HasPrefix(d.StateDir, dir) || !strings.HasPrefix(d.CacheDir, dir) {
		t.Errorf("StateDir %q, CacheDir %q; want both under %s", d.StateDir, d.CacheDir, dir)
	}
	file := filepath.Join(dir, "interleaved.json")
	if want := []string{"OPENCODE_CONFIG=" + file}; !slices.Equal(d.Env, want) {
		t.Fatalf("Env = %q, want %q", d.Env, want)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("%s has mode %v, want 0600: OpenCode reads configuration that can name commands", file, info.Mode().Perm())
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Provider map[string]struct {
			Models map[string]struct {
				Interleaved struct {
					Field string `json:"field"`
				} `json:"interleaved"`
			} `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if f := got.Provider[consumers.Provider].Models[consumers.ChatModel].Interleaved.Field; f != "reasoning_content" {
		t.Errorf("%s: %s's interleaved field is %q, want reasoning_content (%s)", file, consumers.ChatModel, f, b)
	}
}

// TestExecutable shows an executable given is used as is, and otherwise the consumers mise
// environment's is, asked of mise with MISE_ENV=consumers; when mise can't say, the error names
// the flag and variable that set it.
func TestExecutable(t *testing.T) {
	if got, err := consumers.Executable(t.Context(), "/bin/pi", "pi"); err != nil || got != "/bin/pi" {
		t.Errorf("Executable given /bin/pi = %q, %v; want /bin/pi", got, err)
	}

	bin := t.TempDir()
	mise := "#!/bin/sh\n[ \"$1\" = which ] && [ \"$MISE_ENV\" = consumers ] || exit 1\necho \"  /pinned/$2  \"\n"
	if err := os.WriteFile(filepath.Join(bin, "mise"), []byte(mise), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("MISE_ENV", "")
	if got, err := consumers.Executable(t.Context(), "", "claude"); err != nil || got != "/pinned/claude" {
		t.Errorf("Executable = %q, %v; want the pinned /pinned/claude", got, err)
	}

	if err := os.WriteFile(filepath.Join(bin, "mise"), []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := consumers.Executable(t.Context(), "", "opencode")
	if err == nil || !strings.Contains(err.Error(), "MISE_ENV=consumers mise which opencode") || !strings.Contains(err.Error(), "set -opencode or SPIKE_OPENCODE") {
		t.Errorf("Executable without mise's answer = %v, want an error naming the lookup, -opencode and SPIKE_OPENCODE", err)
	}
}

// TestPinsMatchConsumersEnvironment shows each consumer's pin, which a passk cell's Pinned names
// and a run's report checks the running version against, is the version the consumers mise
// environment installs, so bumping one without the other fails here.
func TestPinsMatchConsumersEnvironment(t *testing.T) {
	var env struct {
		Tools map[string]string `toml:"tools"`
	}
	if _, err := toml.DecodeFile("../mise.consumers.toml", &env); err != nil {
		t.Fatal(err)
	}
	for tool, pin := range map[string]string{
		"aqua:earendil-works/pi":      consumers.PiVersion,
		"aqua:anthropics/claude-code": consumers.ClaudeVersion,
		"aqua:anomalyco/opencode":     consumers.OpenCodeVersion,
	} {
		if got := env.Tools[tool]; got != pin {
			t.Errorf("mise.consumers.toml pins %s at %q, the consumers package at %q", tool, got, pin)
		}
	}
}
