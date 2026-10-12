package passk

import (
	"context"
	"encoding/json"
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
	"github.com/JaimeStill/spike-harness-driver/opencode"
	"github.com/JaimeStill/spike-harness-driver/pi"

	"github.com/JaimeStill/spike-model-hosting/concurrent"
)

// The router's models the cells run on, by router ID, which clutch's llama.cpp target names
// too. Each must be loaded already: a run never loads one.
const (
	// ChatModel is every consumer's session model, gpt-oss-120b.
	ChatModel = "ggml-org/gpt-oss-120b-GGUF:MXFP4"
	// VisionModel is the model a session sent an image runs on, Gemma 4 26B-A4B.
	VisionModel = "ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0"
	// AudioModel is the model the audio-tool capability's tool transcribes with, Gemma 4 E4B.
	AudioModel = "ggml-org/gemma-4-E4B-it-GGUF:Q8_0"
	// EmbedModel is the embedding model the direct client names; no capability embeds.
	EmbedModel = "ggml-org/embeddinggemma-2-GGUF:Q8_0"
)

// Models lists the models a run needs loaded.
var Models = []string{ChatModel, VisionModel, AudioModel, EmbedModel}

// Provider is the provider ID Pi's and OpenCode's sessions name: Pi's built-in llama.cpp
// provider, and the OpenAI-compatible provider the OpenCode driver writes under the same name.
const Provider = "llama.cpp"

// The consumer versions the consumers mise environment pins, which each cell's Pinned names.
const (
	PiVersion       = "1.1.0"
	ClaudeVersion   = "2.1.296"
	OpenCodeVersion = "1.19.0"
)

// claudeLogin is the file Claude Code keeps its subscription login in, in its configuration
// directory, on Linux.
const claudeLogin = ".credentials.json"

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
}

// Pi returns Pi's cell, on PiDriver.
func Pi(cfg Config) conform.Cell {
	return cell(cfg, "pi", PiVersion, scenario.Pi, Provider, PiDriver(cfg), []scenario.Need{privateDir(filepath.Join(cfg.State, "pi"))})
}

// PiDriver returns Pi's driver: Pi on its llama.cpp provider pointed at cfg.Router, its agent
// directory, sessions and cache under cfg.State/pi. The driver's bridge sets tool_choice
// "required" on every request that offers respond, as align settled.
func PiDriver(cfg Config) pi.Driver {
	root := filepath.Join(cfg.State, "pi")
	return pi.Driver{
		Command:    cfg.Command,
		AgentDir:   filepath.Join(root, "agent"),
		SessionDir: filepath.Join(root, "sessions"),
		CacheDir:   filepath.Join(root, "cache"),
		// Pi's llama.cpp provider takes the router's root.
		Env: []string{"LLAMA_BASE_URL=" + cfg.Router},
		// Pi starts from an empty catalog in its own agent directory and lists the router's
		// models once its background refresh lands.
		CatalogWait: 30 * time.Second,
	}
}

// OpenCode returns OpenCode's cell, on OpenCodeDriver. It fails when OpenCodeDriver does.
func OpenCode(cfg Config) (conform.Cell, error) {
	d, err := OpenCodeDriver(cfg)
	if err != nil {
		return conform.Cell{}, err
	}
	return cell(cfg, "opencode", OpenCodeVersion, scenario.OpenCode, Provider, d, nil), nil
}

// OpenCodeDriver returns OpenCode's driver: OpenCode on an OpenAI-compatible provider at
// cfg.Router/v1, its configuration, data and sessions under cfg.State/opencode, with
// gpt-oss-120b's interleaved setting naming reasoning_content. The driver writes its own
// configuration into OPENCODE_CONFIG_CONTENT; the setting goes in the explicit configuration
// file OPENCODE_CONFIG names, which OpenCode merges beneath it, model by model. It fails when
// that file can't be written.
func OpenCodeDriver(cfg Config) (opencode.Driver, error) {
	root := filepath.Join(cfg.State, "opencode")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return opencode.Driver{}, err
	}
	interleaved := filepath.Join(root, "interleaved.json")
	if err := os.WriteFile(interleaved, InterleavedConfig(), 0o600); err != nil {
		return opencode.Driver{}, err
	}
	return opencode.Driver{
		Command:  cfg.Command,
		StateDir: filepath.Join(root, "state"),
		CacheDir: filepath.Join(root, "cache"),
		Providers: map[string]opencode.Provider{Provider: {
			BaseURL: strings.TrimSuffix(cfg.Router, "/") + "/v1",
			// The session's model, which the driver lists as text only, and the vision model.
			Models: map[string]opencode.Model{VisionModel: {Image: true}},
		}},
		Env: []string{"OPENCODE_CONFIG=" + interleaved},
	}, nil
}

// InterleavedConfig is the OpenCode configuration that sets gpt-oss-120b's interleaved field to
// reasoning_content: OpenCode then moves each assistant message's reasoning into that field of
// the request, which llama.cpp reads back into gpt-oss's prompt.
func InterleavedConfig() []byte {
	b, _ := json.Marshal(map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"provider": map[string]any{Provider: map[string]any{
			"models": map[string]any{ChatModel: map[string]any{
				"interleaved": map[string]string{"field": "reasoning_content"},
			}},
		}},
	})
	return b
}

// Claude returns Claude Code's cell over the router, on ClaudeDriver. Claude Code drops no
// image, so its dropped-image capability is n/a. The cell is skipped when cfg.ClaudeConfig
// holds no login.
func Claude(cfg Config) conform.Cell {
	login := scenario.Need{
		What: "a Claude Code login in " + cfg.ClaudeConfig,
		Check: func(context.Context) error {
			if _, err := os.Stat(filepath.Join(cfg.ClaudeConfig, claudeLogin)); err != nil {
				return fmt.Errorf("%w: sign in once with `CLAUDE_CONFIG_DIR=%s MISE_ENV=consumers mise exec -- claude`, then /login", err, cfg.ClaudeConfig)
			}
			return nil
		},
	}
	return cell(cfg, "claude", ClaudeVersion, scenario.Claude, claude.Provider, ClaudeDriver(cfg), []scenario.Need{privateDir(filepath.Join(cfg.State, "claude")), login})
}

// ClaudeDriver returns Claude Code's driver over the router: Claude Code from cfg.ClaudeConfig,
// on its subscription login with no API key or token, its model requests sent to cfg.Router as
// gpt-oss-120b (concurrent.ClaudeCode), its skill plugin cached under cfg.State/claude.
func ClaudeDriver(cfg Config) claude.Driver {
	d := concurrent.ClaudeCode(cfg.Command, cfg.ClaudeConfig, cfg.Router, ChatModel)
	d.CacheDir = filepath.Join(cfg.State, "claude", "cache")
	return d
}

// cell builds a consumer's cell on driver d. Every session opens on ChatModel, in a working
// directory of the consumer's own under cfg.State, with its exchanges kept under
// cfg.State/exchanges; needs run after the check that the executable exists.
func cell(cfg Config, name, pinned string, profile scenario.Profile, provider string, d harness.Driver, needs []scenario.Need) conform.Cell {
	work := filepath.Join(cfg.State, "work", name)
	executable := scenario.Need{
		What:  "the " + name + " executable",
		Check: func(context.Context) error { _, err := exec.LookPath(cfg.Command); return err },
	}
	return conform.Cell{
		Harness:  name,
		Provider: Provider,
		Profile:  profile,
		Pinned:   pinned,
		Version:  func(ctx context.Context) (string, error) { return Version(ctx, cfg.Command) },
		Needs:    append([]scenario.Need{executable, privateDir(work)}, needs...),
		Service: session.New(
			func() (harness.Driver, error) { return d, nil },
			func() harness.Options { return SessionOptions(cfg, name, provider) },
		),
		VisionModel: VisionModel,
		Models:      func() (scenario.Models, error) { return directModels(cfg.Direct) },
	}
}

// SessionOptions are the options every session of the consumer name opens with: provider and
// ChatModel, in the consumer's own working directory under cfg.State, its exchanges kept under
// cfg.State/exchanges. They name no tools or skills, which the capabilities add.
func SessionOptions(cfg Config, name, provider string) harness.Options {
	return harness.Options{
		Provider: provider,
		Model:    ChatModel,
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

// directModels returns the direct model client on the router at direct, as clutch's llama.cpp
// target sets it up, with autoload=false on every request, so the router refuses rather than
// loads a model that isn't loaded.
func directModels(direct string) (scenario.Models, error) {
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
		Target:        Provider,
		Chat:          c,
		Audio:         c,
		VisionModel:   VisionModel,
		EmbedModel:    EmbedModel,
		AudioModel:    AudioModel,
		AudioInChat:   true,
		HarnessVision: VisionModel,
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
