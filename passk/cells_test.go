package passk_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-harness-driver/claude"
	"github.com/JaimeStill/spike-harness-driver/clutch/conform"

	"github.com/JaimeStill/spike-model-hosting/consumers"
	"github.com/JaimeStill/spike-model-hosting/passk"
	"github.com/JaimeStill/spike-model-hosting/profile"
)

// config is a cell's Config with the router behind a trailing slash, as a user might give it.
func config(t *testing.T) passk.Config {
	t.Helper()
	state := t.TempDir()
	return passk.Config{
		Command:      "/bin/consumer",
		Router:       "http://127.0.0.1:9999/",
		Direct:       "http://router:8080",
		State:        state,
		ClaudeConfig: filepath.Join(state, "claude-config"),
		Roles:        setA,
	}
}

// setA is set A's role assignment, as the Strix Halo profile names it.
var setA = profile.SetRoles{
	Chat:      "ggml-org/gpt-oss-120b-GGUF:MXFP4",
	Vision:    "ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0",
	Audio:     "ggml-org/gemma-4-E4B-it-GGUF:Q8_0",
	Embedding: "ggml-org/embeddinggemma-2-GGUF:Q8_0",
}

// TestClaudeDriver shows Claude Code runs from the given configuration directory with every model
// request sent to the router as the set's chat model, as consumers.ClaudeEnv points it, its skill
// plugin cached under the run's state directory.
func TestClaudeDriver(t *testing.T) {
	cfg := config(t)
	d := passk.ClaudeDriver(cfg)
	if d.Command != cfg.Command || d.ConfigDir != cfg.ClaudeConfig {
		t.Errorf("Command %q, ConfigDir %q; want %q and %q", d.Command, d.ConfigDir, cfg.Command, cfg.ClaudeConfig)
	}
	if want := consumers.ClaudeEnv(cfg.Router, setA.Chat); !slices.Equal(d.Env, want) {
		t.Errorf("Env = %q, want %q", d.Env, want)
	}
	if !strings.HasPrefix(d.CacheDir, cfg.State) {
		t.Errorf("CacheDir = %q, want it under %s", d.CacheDir, cfg.State)
	}
}

// TestSessionOptions shows each session opens on the set's chat model in a working directory of the
// consumer's own under the state directory, offering no tools or skills of its own.
func TestSessionOptions(t *testing.T) {
	cfg := config(t)
	o := passk.SessionOptions(cfg, "claude", claude.Provider)
	if o.Provider != claude.Provider || o.Model != setA.Chat {
		t.Errorf("Provider %q, Model %q; want %q and %q", o.Provider, o.Model, claude.Provider, setA.Chat)
	}
	if o.Dir != filepath.Join(cfg.State, "work", "claude") {
		t.Errorf("Dir = %q, want the consumer's own under %s", o.Dir, cfg.State)
	}
	if o.Store == nil || o.Tools != nil || o.Skills != nil || o.HarnessTools != nil {
		t.Errorf("Store %v, Tools %v, Skills %v, HarnessTools %v; want a store and nothing else", o.Store, o.Tools, o.Skills, o.HarnessTools)
	}
}

// TestCells shows each cell names its consumer, the version the consumers mise environment pins,
// its consumer's profile, and the set's vision model, and its direct model client names the
// set's models.
func TestCells(t *testing.T) {
	cfg := config(t)
	oc, err := passk.OpenCode(cfg)
	if err != nil {
		t.Fatal(err)
	}
	pi, err := passk.Pi(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		cell                     conform.Cell
		harness, pinned, profile string
	}{
		{pi, "pi", consumers.PiVersion, "Pi"},
		{passk.Claude(cfg), "claude", consumers.ClaudeVersion, "Claude Code"},
		{oc, "opencode", consumers.OpenCodeVersion, "OpenCode"},
	}
	for _, tc := range cases {
		c := tc.cell
		if c.Harness != tc.harness || c.Provider != consumers.Provider || c.Pinned != tc.pinned || c.Profile.Name != tc.profile {
			t.Errorf("%s: Harness %q, Provider %q, Pinned %q, Profile %q", tc.harness, c.Harness, c.Provider, c.Pinned, c.Profile.Name)
		}
		if c.VisionModel != setA.Vision || c.DefaultTakesImages {
			t.Errorf("%s: VisionModel %q, DefaultTakesImages %v; want %s and false", tc.harness, c.VisionModel, c.DefaultTakesImages, setA.Vision)
		}
		m, err := c.Models()
		if err != nil {
			t.Fatalf("%s: Models: %v", tc.harness, err)
		}
		if m.Chat == nil || m.Audio != m.Chat || m.AudioModel != setA.Audio || !m.AudioInChat || m.HarnessVision != setA.Vision ||
			m.VisionModel != setA.Vision || m.EmbedModel != setA.Embedding || m.DefaultTakesImages {
			t.Errorf("%s: Models = %+v; want one client on the router, on set A's models, transcribing with %s in chat", tc.harness, m, setA.Audio)
		}
	}
}

// TestCellsChatTakesImages shows a set whose chat model is its vision model too, as a candidate
// set's is, runs every session and every image on that model, with no text-only model left to
// drop an image.
func TestCellsChatTakesImages(t *testing.T) {
	cfg := config(t)
	const candidate = "example/candidate-GGUF:Q4_K_M"
	cfg.Roles = profile.SetRoles{Chat: candidate, Vision: candidate, Embedding: setA.Embedding}
	oc, err := passk.OpenCode(cfg)
	if err != nil {
		t.Fatal(err)
	}
	pi, err := passk.Pi(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []conform.Cell{pi, passk.Claude(cfg), oc} {
		if c.VisionModel != candidate || !c.DefaultTakesImages {
			t.Errorf("%s: VisionModel %q, DefaultTakesImages %v; want %s and true", c.Harness, c.VisionModel, c.DefaultTakesImages, candidate)
		}
		m, err := c.Models()
		if err != nil || m.HarnessVision != candidate || !m.DefaultTakesImages || m.AudioModel != "" {
			t.Errorf("%s: Models = %+v, %v; want %s for images, taking them by default, and no audio model", c.Harness, m, err, candidate)
		}
	}
	if o := passk.SessionOptions(cfg, "pi", consumers.Provider); o.Model != candidate {
		t.Errorf("SessionOptions Model = %q, want %s", o.Model, candidate)
	}
}

// TestCellsReasoning shows the chat model's consumer reasoning setting reaches Pi's settings
// and OpenCode's configuration, under the run's state directory, and Claude Code's driver takes
// none of it.
func TestCellsReasoning(t *testing.T) {
	cfg := config(t)
	cfg.Reasoning = profile.ConsumerReasoning{
		Pi:       "high",
		OpenCode: map[string]any{"reasoningEffort": "high"},
		Reason:   "the template accepts high",
	}
	if _, err := passk.Pi(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := passk.OpenCode(cfg); err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string][]byte{
		filepath.Join(cfg.State, "pi", "agent", "settings.json"): consumers.PiSettings(setA.Chat, "high"),
		filepath.Join(cfg.State, "opencode", "interleaved.json"): consumers.InterleavedConfig(setA.Chat, cfg.Reasoning.OpenCode),
	} {
		got, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%s = %s; want %s", file, got, want)
		}
	}
	if want := consumers.ClaudeEnv(cfg.Router, setA.Chat); !slices.Equal(passk.ClaudeDriver(cfg).Env, want) {
		t.Errorf("Claude Code's Env = %q; want %q, unchanged by the reasoning setting", passk.ClaudeDriver(cfg).Env, want)
	}
}

// TestClaudeWithoutLogin shows a run skips Claude Code's cell, naming the command that signs in,
// when its configuration directory holds no login, before it starts Claude Code at all.
func TestClaudeWithoutLogin(t *testing.T) {
	cfg := config(t)
	cfg.Command = consumer(t, "echo started >> "+filepath.Join(cfg.State, "ran"))
	rep := conform.Run(t.Context(), []conform.Cell{passk.Claude(cfg)}, conform.Options{AnyVersion: true})[0]
	if !strings.Contains(rep.Skipped, "CLAUDE_CONFIG_DIR="+cfg.ClaudeConfig) || !strings.Contains(rep.Skipped, "/login") {
		t.Errorf("Skipped = %q, want the sign-in command for %s", rep.Skipped, cfg.ClaudeConfig)
	}
	if _, err := os.Stat(filepath.Join(cfg.State, "ran")); err == nil {
		t.Error("Claude Code was started without a login")
	}
}

// TestVersion shows a consumer's version is the first line its --version prints that isn't
// blank.
func TestVersion(t *testing.T) {
	cmd := consumer(t, `printf '\n2.1.301 (Claude Code)\nmore\n'`)
	got, err := passk.Version(t.Context(), cmd)
	if err != nil || got != "2.1.301 (Claude Code)" {
		t.Errorf("Version = %q, %v; want the first line that isn't blank", got, err)
	}
	if _, err := passk.Version(t.Context(), consumer(t, "true")); err == nil {
		t.Error("a --version that prints nothing gave no error")
	}
}

// consumer writes a shell script that runs body, and returns its path.
func consumer(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "consumer")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
