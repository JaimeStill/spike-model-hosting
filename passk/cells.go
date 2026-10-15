package passk

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/JaimeStill/spike-harness-driver/claude"
	"github.com/JaimeStill/spike-harness-driver/clutch/conform"
	"github.com/JaimeStill/spike-harness-driver/clutch/domain/session"
	"github.com/JaimeStill/spike-harness-driver/clutch/scenario"
	"github.com/JaimeStill/spike-harness-driver/harness"
	"github.com/JaimeStill/spike-harness-driver/harness/filestore"
	"github.com/JaimeStill/spike-harness-driver/model"

	"github.com/JaimeStill/spike-model-hosting/consumers"
	"github.com/JaimeStill/spike-model-hosting/profile"
)

// modelTimeout bounds one direct model request, from sending it to reading its response: it
// catches a router that has hung, not one that is slow.
const modelTimeout = 10 * time.Minute

// versionTimeout bounds a consumer's --version, which only prints.
const versionTimeout = 30 * time.Second

// Config is what a cell needs: the consumer, where its model requests go, and where its state
// lives.
type Config struct {
	// Command is the consumer's executable.
	Command string
	// Router is the root URL the consumer sends its model requests to: the router, or a
	// wire.Recorder in front of it.
	Router string
	// Direct is the router's root, which the audio-tool capability's direct model client
	// talks to; the client's requests are the capability's tool, not the consumer's.
	Direct string
	// State is the run's untracked state directory, absolute: each consumer keeps its
	// sessions, configuration and cache under its own subdirectory, never the user's.
	State string
	// ClaudeConfig is Claude Code's configuration directory, which holds its subscription
	// login. Only the Claude cell reads it.
	ClaudeConfig string
	// Roles are the models of the profile's set the run is on, by router ID, which clutch's
	// llama.cpp target names too: every session runs on the chat model, a session sent an image
	// on the vision model, the audio-tool capability's tool transcribes with the audio model,
	// and the direct client names the embedding model, though no capability embeds. Each must
	// be loaded already: a run never loads one.
	Roles profile.SetRoles
	// Reasoning is the reasoning setting Pi and OpenCode send for the chat model, its
	// profile.Model's Consumers; the zero value leaves each at its own default. Claude Code
	// takes none.
	Reasoning profile.ConsumerReasoning
}

// Pi returns Pi's cell: Pi on its llama.cpp provider pointed at cfg.Router, at the chat model's
// thinking level, cfg.Reasoning.Pi (consumers.Pi), its agent directory, sessions and cache under
// cfg.State/pi. It fails when Pi's settings file can't be written.
func Pi(cfg Config) (conform.Cell, error) {
	dir := filepath.Join(cfg.State, "pi")
	d, err := consumers.Pi(cfg.Command, cfg.Router, dir, cfg.Roles.Chat, cfg.Reasoning.Pi)
	if err != nil {
		return conform.Cell{}, err
	}
	return cell(cfg, "pi", consumers.PiVersion, scenario.Pi, consumers.Provider, d, []scenario.Need{privateDir(dir)}), nil
}

// OpenCode returns OpenCode's cell: OpenCode on an OpenAI-compatible provider at cfg.Router/v1
// with the chat model's interleaved setting naming reasoning_content and its options,
// cfg.Reasoning.OpenCode (consumers.OpenCode), its
// state under cfg.State/opencode, the vision model, when the set has one, listed as taking
// images. It fails when the driver's configuration file can't be written.
func OpenCode(cfg Config) (conform.Cell, error) {
	var images []string
	if cfg.Roles.Vision != "" {
		images = append(images, cfg.Roles.Vision)
	}
	d, err := consumers.OpenCode(cfg.Command, cfg.Router, filepath.Join(cfg.State, "opencode"), cfg.Roles.Chat, cfg.Reasoning.OpenCode, images...)
	if err != nil {
		return conform.Cell{}, err
	}
	return cell(cfg, "opencode", consumers.OpenCodeVersion, scenario.OpenCode, consumers.Provider, d, nil), nil
}

// Claude returns Claude Code's cell over the router, on ClaudeDriver. Claude Code drops no
// image, so its dropped-image capability is n/a. The cell is skipped when cfg.ClaudeConfig
// holds no login.
func Claude(cfg Config) conform.Cell {
	login := scenario.Need{
		What:  "a Claude Code login in " + cfg.ClaudeConfig,
		Check: func(context.Context) error { return consumers.CheckClaudeLogin(cfg.ClaudeConfig) },
	}
	return cell(cfg, "claude", consumers.ClaudeVersion, scenario.Claude, claude.Provider, ClaudeDriver(cfg), []scenario.Need{privateDir(filepath.Join(cfg.State, "claude")), login})
}

// ClaudeDriver returns Claude Code's driver over the router: Claude Code from cfg.ClaudeConfig,
// on its subscription login with no API key or token, its model requests sent to cfg.Router as
// the chat model (consumers.ClaudeCode), its skill plugin cached under cfg.State/claude.
func ClaudeDriver(cfg Config) claude.Driver {
	d := consumers.ClaudeCode(cfg.Command, cfg.ClaudeConfig, cfg.Router, cfg.Roles.Chat)
	d.CacheDir = filepath.Join(cfg.State, "claude", "cache")
	return d
}

// cell builds a consumer's cell on driver d. Every session opens on the chat model, in a working
// directory of the consumer's own under cfg.State, with its exchanges kept under
// cfg.State/exchanges; needs run after the check that the executable exists. A set whose chat
// model is its vision model too leaves no text-only model to drop an image.
func cell(cfg Config, name, pinned string, profile scenario.Profile, provider string, d harness.Driver, needs []scenario.Need) conform.Cell {
	work := filepath.Join(cfg.State, "work", name)
	executable := scenario.Need{
		What:  "the " + name + " executable",
		Check: func(context.Context) error { _, err := exec.LookPath(cfg.Command); return err },
	}
	return conform.Cell{
		Harness:  name,
		Provider: consumers.Provider,
		Profile:  profile,
		Pinned:   pinned,
		Version:  func(ctx context.Context) (string, error) { return Version(ctx, cfg.Command) },
		Needs:    append([]scenario.Need{executable, privateDir(work)}, needs...),
		Service: session.New(
			func() (harness.Driver, error) { return d, nil },
			func() harness.Options { return SessionOptions(cfg, name, provider) },
		),
		VisionModel:        cfg.Roles.Vision,
		DefaultTakesImages: takesImages(cfg.Roles),
		Models:             func() (scenario.Models, error) { return directModels(cfg.Direct, cfg.Roles) },
	}
}

// SessionOptions are the options every session of the consumer name opens with: provider and
// the chat model, in the consumer's own working directory under cfg.State, its exchanges kept
// under cfg.State/exchanges. They name no tools or skills, which the capabilities add.
func SessionOptions(cfg Config, name, provider string) harness.Options {
	return harness.Options{
		Provider: provider,
		Model:    cfg.Roles.Chat,
		Dir:      filepath.Join(cfg.State, "work", name),
		Store:    filestore.New(filepath.Join(cfg.State, "exchanges")),
	}
}

// privateDir is the need that dir exists and is the user's alone, which the drivers require of
// the directories they run code from.
func privateDir(dir string) scenario.Need {
	return scenario.Need{
		What:  "the directory " + dir,
		Check: func(context.Context) error { return os.MkdirAll(dir, 0o700) },
	}
}

// takesImages reports whether the set's chat model is its vision model too.
func takesImages(r profile.SetRoles) bool { return r.Vision != "" && r.Vision == r.Chat }

// directModels returns the direct model client on the router at direct, as clutch's llama.cpp
// target sets it up, naming the set's role models, with autoload=false on every request, so the
// router refuses rather than loads a model that isn't loaded.
func directModels(direct string, r profile.SetRoles) (scenario.Models, error) {
	if direct == "" {
		return scenario.Models{}, errors.New("no router for the direct model client")
	}
	c, err := model.New(model.Config{
		BaseURL: strings.TrimSuffix(direct, "/") + "/v1",
		Query:   url.Values{"autoload": {"false"}},
		HTTP:    &http.Client{Timeout: modelTimeout},
	})
	if err != nil {
		return scenario.Models{}, err
	}
	return scenario.Models{
		Target:             consumers.Provider,
		Chat:               c,
		Audio:              c,
		VisionModel:        r.Vision,
		EmbedModel:         r.Embedding,
		AudioModel:         r.Audio,
		AudioInChat:        true,
		HarnessVision:      r.Vision,
		DefaultTakesImages: takesImages(r),
	}, nil
}

// Version runs command --version and returns the first line it prints that isn't blank.
func Version(ctx context.Context, command string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, command, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s --version: %w", command, err)
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line, nil
		}
	}
	return "", fmt.Errorf("%s --version printed nothing", command)
}
