// Command concurrent runs Pi, Claude Code and the model client against the router at once, and
// checks that every answer reached its own request.
//
// Usage:
//
//	concurrent [-router url] [-profile path] [-rounds n] [-pi path] [-claude path] [-state dir] [-inject-swap]
//
// It runs four streams at once: Pi over OpenAI chat, Claude Code over Anthropic messages, the
// model client over chat, and the model client over embeddings. The chat streams share the chat
// model's slots, from the profile, so every slot carries a request; each chat worker sends
// -rounds requests, each with its own nonce. The embedding stream sends batch after batch until
// the last chat request returns. The run passes when every answer carries its own nonce and no
// other, and every batched embedding matches its input's lone embedding within the concurrent
// package's EmbedTolerance. -inject-swap swaps two answers before the check, which must then
// fail and name both.
//
// The router comes from -router or SPIKE_ROUTER_URL. Every model the run sends to must be loaded
// already: the tool checks GET /models first and never loads one. Pi is -pi, or SPIKE_PI, and
// Claude Code is -claude, or SPIKE_CLAUDE; each defaults to the one the consumers mise
// environment pins (MISE_ENV=consumers mise which pi, or claude). Their state lives under
// -state, or SPIKE_STATE_DIR, which defaults to the repository's untracked .state/: Pi's agent
// directory, sessions and cache under pi/, never the user's ~/.pi, and Claude Code's
// configuration directory at claude/, never the user's ~/.claude. Claude Code runs on the
// architect's subscription login, made once in that directory:
//
//	CLAUDE_CONFIG_DIR=$PWD/.state/claude MISE_ENV=consumers mise exec -- claude
//
// then /login. Its model requests go to the router, every model alias mapped to the chat model.
//
// It exits 0 when the run passes, 1 when it fails or can't run, and 2 on a usage error.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/JaimeStill/spike-harness-driver/claude"
	"github.com/JaimeStill/spike-harness-driver/harness"
	"github.com/JaimeStill/spike-harness-driver/model"
	"github.com/JaimeStill/spike-harness-driver/pi"

	"github.com/JaimeStill/spike-model-hosting/concurrent"
	"github.com/JaimeStill/spike-model-hosting/profile"
	"github.com/JaimeStill/spike-model-hosting/router"
)

// chatModel is the router ID the chat streams send to, gpt-oss-120b, the harness model. The
// profile types no role, so the tool names it, and fails if the profile doesn't list it.
const chatModel = "ggml-org/gpt-oss-120b-GGUF:MXFP4"

// maxTokens caps a model-client chat completion, reasoning included: room for gpt-oss-120b's
// reasoning before a one-token answer.
const maxTokens = 4096

// llamaProvider is Pi's built-in provider for a llama.cpp server, which speaks OpenAI chat to
// LLAMA_BASE_URL.
const llamaProvider = "llama.cpp"

// inputs are the embedding stream's texts: distinct sentences, so each input's vector is told
// from every other's.
var inputs = []string{
	"The access code is seven four two nine.",
	"A red circle, a blue square, and a green triangle.",
	"The router serves four models side by side.",
	"Unified memory holds the weights and the KV cache together.",
	"Each request carries a nonce unique to the run.",
	"Rain is expected in the mountains by Thursday evening.",
	"She tuned the violin before the concert began.",
	"Compile the program, then run its tests.",
}

// errUsage marks an error in how the tool was called, which exits 2 rather than 1.
var errUsage = errors.New("usage")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// usage is the tool's usage, printed on -h and after a usage error.
const usage = `usage:
  concurrent [-router url] [-profile path] [-rounds n] [-pi path] [-claude path] [-state dir] [-inject-swap]
`

// options are the tool's flags, after the environment's fallbacks.
type options struct {
	router, profile, pi, claude, state string
	rounds                             int
	timeout                            time.Duration
	injectSwap                         bool
}

// run runs the tool with args, reading the environment through getenv, and returns its exit
// status.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	opts, err := parse(args, getenv)
	if err == nil {
		var rep concurrent.Report
		rep, err = runOnce(ctx, opts, stdout)
		if err == nil {
			if !rep.Passed() {
				return 1
			}
			return 0
		}
	}
	// A failed write to stderr has nowhere left to be reported, so its errors are dropped.
	switch {
	case errors.Is(err, flag.ErrHelp):
		if _, err := fmt.Fprint(stdout, usage); err != nil {
			return 1
		}
		return 0
	case errors.Is(err, errUsage):
		_, _ = fmt.Fprintf(stderr, "concurrent: %v\n%s", err, usage)
		return 2
	default:
		_, _ = fmt.Fprintf(stderr, "concurrent: %v\n", err)
		return 1
	}
}

// parse reads the flags in args, each falling back to its environment variable.
func parse(args []string, getenv func(string) string) (options, error) {
	fs := flag.NewFlagSet("concurrent", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	var o options
	fs.StringVar(&o.router, "router", getenv("SPIKE_ROUTER_URL"), "the router's root URL (SPIKE_ROUTER_URL)")
	fs.StringVar(&o.profile, "profile", "profiles/strix-halo.toml", "the profile the router serves")
	fs.IntVar(&o.rounds, "rounds", 3, "the requests each chat worker sends")
	fs.StringVar(&o.pi, "pi", getenv("SPIKE_PI"), "the Pi executable (SPIKE_PI); empty asks mise for the pinned one")
	fs.StringVar(&o.claude, "claude", getenv("SPIKE_CLAUDE"), "the Claude Code executable (SPIKE_CLAUDE); empty asks mise for the pinned one")
	fs.StringVar(&o.state, "state", cmp.Or(getenv("SPIKE_STATE_DIR"), ".state"), "the consumers' untracked state directory (SPIKE_STATE_DIR)")
	fs.DurationVar(&o.timeout, "timeout", concurrent.DefaultRequestTimeout, "the bound on one request")
	fs.BoolVar(&o.injectSwap, "inject-swap", false, "swap two answers before the check, which must then fail")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return o, err
		}
		return o, fmt.Errorf("%w: %v", errUsage, err)
	}
	switch {
	case fs.NArg() != 0:
		return o, fmt.Errorf("%w: unexpected arguments %q", errUsage, fs.Args())
	case o.router == "":
		return o, fmt.Errorf("%w: no router: set -router or SPIKE_ROUTER_URL", errUsage)
	case o.rounds < 1:
		return o, fmt.Errorf("%w: -rounds must be at least 1", errUsage)
	}
	o.router = strings.TrimSuffix(o.router, "/")
	// Pi and Claude Code run in working directories of their own and read the paths their
	// drivers give them from there, so the state directory is made absolute.
	state, err := filepath.Abs(o.state)
	if err != nil {
		return o, err
	}
	o.state = state
	return o, nil
}

// runOnce runs the streams once and writes the report.
func runOnce(ctx context.Context, o options, stdout io.Writer) (concurrent.Report, error) {
	p, err := profile.Load(o.profile)
	if err != nil {
		return concurrent.Report{}, err
	}
	embedModel, err := checkProfile(p)
	if err != nil {
		return concurrent.Report{}, err
	}
	hc := &http.Client{}
	// Pi's provider can't carry autoload=false, so the run never starts against a model a request
	// would load.
	if err := router.CheckLoaded(ctx, hc, o.router, chatModel, embedModel); err != nil {
		return concurrent.Report{}, err
	}
	build, err := router.Build(ctx, hc, o.router)
	if err != nil {
		return concurrent.Report{}, err
	}
	piCmd, err := resolveTool(ctx, o.pi, "pi", "-pi or SPIKE_PI")
	if err != nil {
		return concurrent.Report{}, err
	}
	piDriver, err := newPiDriver(o, piCmd)
	if err != nil {
		return concurrent.Report{}, err
	}
	claudeCmd, err := resolveTool(ctx, o.claude, "claude", "-claude or SPIKE_CLAUDE")
	if err != nil {
		return concurrent.Report{}, err
	}
	claudeDriver, err := newClaudeDriver(o, claudeCmd)
	if err != nil {
		return concurrent.Report{}, err
	}
	client, err := model.New(model.Config{
		BaseURL: o.router + "/v1",
		// The router refuses, rather than loads, a model that isn't loaded.
		Query: url.Values{"autoload": {"false"}},
		HTTP:  hc,
	})
	if err != nil {
		return concurrent.Report{}, err
	}

	workers := concurrent.SplitSlots(p.Slots, 3)
	cfg := concurrent.Config{
		Rounds:         o.rounds,
		RequestTimeout: o.timeout,
		InjectSwap:     o.injectSwap,
		Chat: []concurrent.ChatStream{
			{Name: "pi", Workers: workers[0], Chatter: concurrent.HarnessChat{
				Driver: piDriver,
				Options: harness.Options{
					Provider: llamaProvider, Model: chatModel,
					Dir:          filepath.Join(o.state, "pi", "work"),
					HarnessTools: []string{},
				},
			}},
			{Name: "claude", Workers: workers[1], Chatter: concurrent.HarnessChat{
				Driver: claudeDriver,
				Options: harness.Options{
					Provider: claude.Provider, Model: chatModel,
					Dir:          filepath.Join(o.state, "claude-work"),
					HarnessTools: []string{},
				},
			}},
			{Name: "model-chat", Workers: workers[2], Chatter: concurrent.ModelChat{
				Client: client, Model: chatModel, MaxTokens: maxTokens,
			}},
		},
		Embed: []concurrent.EmbedStream{
			{Name: "model-embed", Embedder: concurrent.ModelEmbed{Client: client, Model: embedModel}, Inputs: inputs},
		},
	}
	if _, err := fmt.Fprintf(stdout, "router %s, build %s\nchat %s on %d slots: pi %d, claude %d, model-chat %d workers, %d rounds\nembed %s: %d inputs, batch after batch until the chats finish, at least %d rounds\n\n",
		o.router, build, chatModel, p.Slots, workers[0], workers[1], workers[2], o.rounds, embedModel, len(inputs), o.rounds); err != nil {
		return concurrent.Report{}, err
	}
	rep, err := concurrent.Run(ctx, cfg)
	if err != nil {
		return rep, err
	}
	return rep, write(stdout, rep, build)
}

// checkProfile checks the profile lists the chat model and returns its embedding model.
func checkProfile(p *profile.Profile) (string, error) {
	var embed string
	listed := false
	for _, m := range p.Models {
		listed = listed || m.ID == chatModel
		if m.Kind == profile.KindEmbedding {
			embed = m.ID
		}
	}
	switch {
	case !listed:
		return "", fmt.Errorf("the profile doesn't list %s, which the chat streams send to", chatModel)
	case embed == "":
		return "", errors.New("the profile lists no embedding model")
	}
	return embed, nil
}

// resolveTool returns a consumer's executable: given, or else the one named name that the
// consumers mise environment pins, rather than whatever the user's PATH finds first. override
// names the flag and variable that set it.
func resolveTool(ctx context.Context, given, name, override string) (string, error) {
	if given != "" {
		return given, nil
	}
	cmd := exec.CommandContext(ctx, "mise", "which", name)
	cmd.Env = append(os.Environ(), "MISE_ENV=consumers")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("finding the pinned %s (MISE_ENV=consumers mise which %s): %w; set %s", name, name, err, override)
	}
	return strings.TrimSpace(string(out)), nil
}

// newPiDriver returns Pi's driver, its agent directory, sessions and cache under the state
// directory, on Pi's llama.cpp provider pointed at the router.
func newPiDriver(o options, command string) (pi.Driver, error) {
	root := filepath.Join(o.state, "pi")
	// Pi runs code from its agent directory and cache, so the driver refuses either unless it
	// is private to the user.
	for _, dir := range []string{root, filepath.Join(root, "work")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return pi.Driver{}, err
		}
	}
	return pi.Driver{
		Command:    command,
		AgentDir:   filepath.Join(root, "agent"),
		SessionDir: filepath.Join(root, "sessions"),
		CacheDir:   filepath.Join(root, "cache"),
		// Pi's llama.cpp provider takes the router's root.
		Env: []string{"LLAMA_BASE_URL=" + o.router},
		// Pi starts with an empty catalog in its own agent directory and lists the router's
		// models once its background refresh lands; two sessions starting at once share the
		// router's answer time.
		CatalogWait: 30 * time.Second,
	}, nil
}

// claudeLogin is the file Claude Code writes its subscription login to, in its configuration
// directory, on Linux.
const claudeLogin = ".credentials.json"

// newClaudeDriver returns Claude Code's driver, its configuration directory under the state
// directory, its model requests sent to the router. It fails, naming the command that signs in,
// when that directory holds no login, rather than start a run in which every Claude Code request
// fails.
func newClaudeDriver(o options, command string) (claude.Driver, error) {
	dir := filepath.Join(o.state, "claude")
	for _, d := range []string{dir, filepath.Join(o.state, "claude-work")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return claude.Driver{}, err
		}
	}
	if _, err := os.Stat(filepath.Join(dir, claudeLogin)); err != nil {
		return claude.Driver{}, fmt.Errorf("claude code has no login in %s (%w): sign in once with "+
			"`CLAUDE_CONFIG_DIR=%s MISE_ENV=consumers mise exec -- claude`, then /login", dir, err, dir)
	}
	return concurrent.ClaudeCode(command, dir, o.router, chatModel), nil
}

// write writes the report: one line per request, then the failures and the verdict.
func write(w io.Writer, rep concurrent.Report, build string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "REQUEST\tSTART\tTOOK\tRESULT\tDETAIL")
	for _, c := range rep.Chats {
		verdict := "ok"
		if !c.Passed() {
			verdict = "FAIL"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s %q\n", c.ID, round(c.Start), round(c.Duration), verdict, c.Nonce, oneLine(c.Answer))
	}
	for _, e := range rep.Embeds {
		verdict := "ok"
		if !e.Passed() {
			verdict = "FAIL"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\tcosine %.6f\n", e.ID, round(e.Start), round(e.Duration), verdict, e.Cosine)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(rep.Swapped) > 0 {
		_, _ = fmt.Fprintf(w, "\ninjected swap: %s and %s\n", rep.Swapped[0], rep.Swapped[1])
	}
	if f := rep.Failures(); len(f) > 0 {
		_, _ = fmt.Fprintln(w, "\nfailures:")
		for _, line := range f {
			_, _ = fmt.Fprintln(w, "  "+line)
		}
	}
	result := "PASS"
	if !rep.Passed() {
		result = "FAIL"
	}
	_, err := fmt.Fprintf(w, "\n%s: %d chat requests, %d embeddings in %s, min embedding cosine %.6f, build %s\n",
		result, len(rep.Chats), len(rep.Embeds), round(rep.Elapsed), minCosine(rep), build)
	return err
}

func round(d time.Duration) time.Duration { return d.Round(10 * time.Millisecond) }

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func minCosine(rep concurrent.Report) float64 {
	low := 1.0
	for _, e := range rep.Embeds {
		if e.Err == nil {
			low = min(low, e.Cosine)
		}
	}
	return low
}
