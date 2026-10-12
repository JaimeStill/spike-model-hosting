package passk_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-harness-driver/claude"
	"github.com/JaimeStill/spike-harness-driver/clutch/conform"

	"github.com/JaimeStill/spike-model-hosting/concurrent"
	"github.com/JaimeStill/spike-model-hosting/passk"
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
	}
}

// TestPiDriver shows Pi runs the given executable on its llama.cpp provider at the router, its
// agent directory, sessions and cache all under the run's state directory, never ~/.pi.
func TestPiDriver(t *testing.T) {
	cfg := config(t)
	d := passk.PiDriver(cfg)
	root := filepath.Join(cfg.State, "pi")
	if d.Command != cfg.Command {
		t.Errorf("Command = %q, want %q", d.Command, cfg.Command)
	}
	for name, got := range map[string]string{"AgentDir": d.AgentDir, "SessionDir": d.SessionDir, "CacheDir": d.CacheDir} {
		if filepath.Dir(got) != root {
			t.Errorf("%s = %q, want a directory in %s", name, got, root)
		}
	}
	if want := []string{"LLAMA_BASE_URL=" + cfg.Router}; !slices.Equal(d.Env, want) {
		t.Errorf("Env = %q, want %q", d.Env, want)
	}
}

// TestOpenCodeDriver shows OpenCode talks to the router's OpenAI-compatible base, lists the
// vision model as taking images, keeps its state under the run's own directory, and loads the
// configuration file that sets gpt-oss-120b's interleaved field to reasoning_content.
func TestOpenCodeDriver(t *testing.T) {
	cfg := config(t)
	d, err := passk.OpenCodeDriver(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := d.Providers[passk.Provider]
	if !ok || len(d.Providers) != 1 {
		t.Fatalf("Providers = %v, want only %q", d.Providers, passk.Provider)
	}
	if p.BaseURL != "http://127.0.0.1:9999/v1" {
		t.Errorf("BaseURL = %q, want the router's /v1", p.BaseURL)
	}
	if m, ok := p.Models[passk.VisionModel]; !ok || !m.Image {
		t.Errorf("Models = %v, want %s taking images", p.Models, passk.VisionModel)
	}
	if !strings.HasPrefix(d.StateDir, cfg.State) || !strings.HasPrefix(d.CacheDir, cfg.State) {
		t.Errorf("StateDir %q, CacheDir %q; want both under %s", d.StateDir, d.CacheDir, cfg.State)
	}
	file := filepath.Join(cfg.State, "opencode", "interleaved.json")
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
	if f := got.Provider[passk.Provider].Models[passk.ChatModel].Interleaved.Field; f != "reasoning_content" {
		t.Errorf("%s: %s's interleaved field is %q, want reasoning_content (%s)", file, passk.ChatModel, f, b)
	}
}

// TestClaudeDriver shows Claude Code runs from the given configuration directory with every
// model request sent to the router as gpt-oss-120b, and no API key or token in its environment,
// which would take the place of the subscription login.
func TestClaudeDriver(t *testing.T) {
	cfg := config(t)
	d := passk.ClaudeDriver(cfg)
	if d.Command != cfg.Command || d.ConfigDir != cfg.ClaudeConfig {
		t.Errorf("Command %q, ConfigDir %q; want %q and %q", d.Command, d.ConfigDir, cfg.Command, cfg.ClaudeConfig)
	}
	if want := concurrent.ClaudeEnv(cfg.Router, passk.ChatModel); !slices.Equal(d.Env, want) {
		t.Errorf("Env = %q, want %q", d.Env, want)
	}
	for _, v := range d.Env {
		if strings.HasPrefix(v, "ANTHROPIC_API_KEY=") || strings.HasPrefix(v, "ANTHROPIC_AUTH_TOKEN=") {
			t.Errorf("Env carries %q, which replaces the subscription login", v)
		}
	}
	if !strings.HasPrefix(d.CacheDir, cfg.State) {
		t.Errorf("CacheDir = %q, want it under %s", d.CacheDir, cfg.State)
	}
}

// TestSessionOptions shows each session opens on gpt-oss-120b in a working directory of the
// consumer's own under the state directory, offering no tools or skills of its own.
func TestSessionOptions(t *testing.T) {
	cfg := config(t)
	o := passk.SessionOptions(cfg, "claude", claude.Provider)
	if o.Provider != claude.Provider || o.Model != passk.ChatModel {
		t.Errorf("Provider %q, Model %q; want %q and %q", o.Provider, o.Model, claude.Provider, passk.ChatModel)
	}
	if o.Dir != filepath.Join(cfg.State, "work", "claude") {
		t.Errorf("Dir = %q, want the consumer's own under %s", o.Dir, cfg.State)
	}
	if o.Store == nil || o.Tools != nil || o.Skills != nil || o.HarnessTools != nil {
		t.Errorf("Store %v, Tools %v, Skills %v, HarnessTools %v; want a store and nothing else", o.Store, o.Tools, o.Skills, o.HarnessTools)
	}
}

// TestCells shows each cell names its consumer, the version the consumers mise environment pins,
// its consumer's profile, and the router's vision model, and its direct model client names the
// router's models.
func TestCells(t *testing.T) {
	cfg := config(t)
	oc, err := passk.OpenCode(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		cell                     conform.Cell
		harness, pinned, profile string
	}{
		{passk.Pi(cfg), "pi", passk.PiVersion, "Pi"},
		{passk.Claude(cfg), "claude", passk.ClaudeVersion, "Claude Code"},
		{oc, "opencode", passk.OpenCodeVersion, "OpenCode"},
	}
	for _, tc := range cases {
		c := tc.cell
		if c.Harness != tc.harness || c.Provider != passk.Provider || c.Pinned != tc.pinned || c.Profile.Name != tc.profile {
			t.Errorf("%s: Harness %q, Provider %q, Pinned %q, Profile %q", tc.harness, c.Harness, c.Provider, c.Pinned, c.Profile.Name)
		}
		if c.VisionModel != passk.VisionModel || c.DefaultTakesImages {
			t.Errorf("%s: VisionModel %q, DefaultTakesImages %v; want %s and false", tc.harness, c.VisionModel, c.DefaultTakesImages, passk.VisionModel)
		}
		m, err := c.Models()
		if err != nil {
			t.Fatalf("%s: Models: %v", tc.harness, err)
		}
		if m.Chat == nil || m.Audio != m.Chat || m.AudioModel != passk.AudioModel || !m.AudioInChat || m.HarnessVision != passk.VisionModel {
			t.Errorf("%s: Models = %+v; want one client on the router, transcribing with %s in chat", tc.harness, m, passk.AudioModel)
		}
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
	cmd := consumer(t, `printf '\n2.1.296 (Claude Code)\nmore\n'`)
	got, err := passk.Version(t.Context(), cmd)
	if err != nil || got != "2.1.296 (Claude Code)" {
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
