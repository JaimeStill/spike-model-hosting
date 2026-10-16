package consumers_test

import (
	"bytes"
	"encoding/json"
	"maps"
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

// chatModel is a set's chat model, which the profile names and the consumers take.
const chatModel = "ggml-org/gpt-oss-120b-GGUF:MXFP4"

// TestPi shows Pi runs the given executable on its llama.cpp provider at the router, its agent
// directory, sessions and cache all under the given directory, never ~/.pi, with the chat
// model's thinking level in the agent directory's settings, which only the user can read.
func TestPi(t *testing.T) {
	dir := t.TempDir()
	d, err := consumers.Pi("/bin/pi", router, dir, chatModel, "medium")
	if err != nil {
		t.Fatal(err)
	}
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
	if info, err := os.Stat(d.AgentDir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("agent directory %s: %v, %v; want it made, mode 0700: Pi runs code from it", d.AgentDir, info, err)
	}
	file := filepath.Join(d.AgentDir, "settings.json")
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("%s has mode %v, want 0600", file, info.Mode().Perm())
	}
	if got := piThinking(t, file); !maps.Equal(got, map[string]string{"llama.cpp/" + chatModel: "medium"}) {
		t.Errorf("%s modelThinkingLevels = %v, want the chat model at medium", file, got)
	}
}

// TestPiNoThinking shows a chat model with no thinking level of its own leaves Pi at its
// default, the settings file naming none, and that the file replaces one a run before left.
func TestPiNoThinking(t *testing.T) {
	dir := t.TempDir()
	if _, err := consumers.Pi("/bin/pi", router, dir, "example/candidate-GGUF:Q4_K_M", "high"); err != nil {
		t.Fatal(err)
	}
	d, err := consumers.Pi("/bin/pi", router, dir, chatModel, "")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(d.AgentDir, "settings.json")
	if got := piThinking(t, file); len(got) != 0 {
		t.Errorf("%s modelThinkingLevels = %v, want none: the earlier run's setting replaced", file, got)
	}
	if got := string(consumers.PiSettings(chatModel, "")); got != "{}" {
		t.Errorf("PiSettings(no level) = %s, want {}", got)
	}
}

// piThinking reads the modelThinkingLevels of the Pi settings file at path.
func piThinking(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		ModelThinkingLevels map[string]string `json:"modelThinkingLevels"`
	}
	if err := json.Unmarshal(b, &settings); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return settings.ModelThinkingLevels
}

// TestClaudeCode shows Claude Code's driver runs the given executable from the run's own
// configuration directory, sends every model request to the router as the one model, whichever
// alias asks, and carries nothing else: no API key or token, which would take the place of the
// subscription login.
func TestClaudeCode(t *testing.T) {
	d := consumers.ClaudeCode("/bin/claude", "/repo/.state/claude", router, chatModel)
	if d.Command != "/bin/claude" || d.ConfigDir != "/repo/.state/claude" {
		t.Errorf("Command %q, ConfigDir %q; want the executable and the configuration directory given", d.Command, d.ConfigDir)
	}
	want := []string{
		"ANTHROPIC_BASE_URL=" + router,
		"ANTHROPIC_MODEL=" + chatModel,
		"ANTHROPIC_DEFAULT_OPUS_MODEL=" + chatModel,
		"ANTHROPIC_DEFAULT_SONNET_MODEL=" + chatModel,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL=" + chatModel,
	}
	if !slices.Equal(d.Env, want) {
		t.Errorf("Env = %q, want %q", d.Env, want)
	}
	if !slices.Equal(consumers.ClaudeEnv(router, chatModel), want) {
		t.Errorf("ClaudeEnv = %q, want %q", consumers.ClaudeEnv(router, chatModel), want)
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
// configuration file that sets the chat model's interleaved field to reasoning_content.
func TestOpenCode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "opencode")
	const vision = "ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0"
	d, err := consumers.OpenCode("/bin/opencode", router, dir, chatModel, nil, vision)
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
	if f := got.Provider[consumers.Provider].Models[chatModel].Interleaved.Field; f != "reasoning_content" {
		t.Errorf("%s: %s's interleaved field is %q, want reasoning_content (%s)", file, chatModel, f, b)
	}
	if bytes.Contains(b, []byte(`"options"`)) {
		t.Errorf("%s = %s; want no options for a chat model given none", file, b)
	}
}

// TestOpenCodeOptions shows a chat model's options, nested ones included, are set on its entry
// beside its interleaved field, where OpenCode sends them with each of its requests.
func TestOpenCodeOptions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "opencode")
	options := map[string]any{
		"reasoningEffort":      "high",
		"chat_template_kwargs": map[string]any{"enable_thinking": true},
	}
	if _, err := consumers.OpenCode("/bin/opencode", router, dir, chatModel, options); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "interleaved.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := `"` + chatModel + `":{"interleaved":{"field":"reasoning_content"},"options":{"chat_template_kwargs":{"enable_thinking":true},"reasoningEffort":"high"}}`
	if !bytes.Contains(b, []byte(want)) {
		t.Errorf("interleaved.json = %s; want it to hold %s", b, want)
	}
}

// TestOpenCodeChatTakesImages shows a chat model that is also the image model, as a candidate
// main model is, is listed once, taking images, and its interleaved setting names its own ID.
func TestOpenCodeChatTakesImages(t *testing.T) {
	const candidate = "example/candidate-GGUF:Q4_K_M"
	dir := filepath.Join(t.TempDir(), "opencode")
	d, err := consumers.OpenCode("/bin/opencode", router, dir, candidate, nil, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if m := d.Providers[consumers.Provider].Models; len(m) != 1 || !m[candidate].Image {
		t.Errorf("Models = %v, want only %s, taking images", m, candidate)
	}
	b, err := os.ReadFile(filepath.Join(dir, "interleaved.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"`+candidate+`":{"interleaved":{"field":"reasoning_content"}}`)) {
		t.Errorf("interleaved.json = %s; want %s's interleaved field set", b, candidate)
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
