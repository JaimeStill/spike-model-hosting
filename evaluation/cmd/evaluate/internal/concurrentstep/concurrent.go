// Package concurrentstep is evaluate's concurrent step, which runs Pi, Claude Code and the model
// client against the router at once, and checks that every answer reached its own request.
//
// Usage:
//
//	evaluate concurrent [-router url] [-profile name|path] [-set name] [-rounds n] [-pi path] [-claude path] [-state dir] [-inject-swap]
//
// It runs four streams at once: Pi over OpenAI chat, Claude Code over Anthropic messages, the model
// client over chat, and the model client over embeddings. The chat streams send to the chat model,
// and the embedding stream to the embedding model, of the profile's set -set names, or SPIKE_SET,
// or else of its startup set. The chat streams share the chat model's slots, from the profile, so
// every slot carries a request; each chat worker sends -rounds requests, each with its own nonce.
// The embedding stream sends batch after batch until the last chat request returns. The run passes
// when every answer carries its own nonce and no other, and every batched embedding matches its
// input's lone embedding within the concurrent package's EmbedTolerance. -inject-swap swaps two
// answers before the check, which must then fail and name both.
//
// The router comes from -router or SPIKE_ROUTER_URL. Every model of the set must be loaded already:
// the step checks GET /models first and never loads one. The report names the build and the set it
// ran on. Pi is -pi, or SPIKE_PI, and Claude Code is -claude, or SPIKE_CLAUDE; each defaults to the
// one the consumers mise environment pins (MISE_ENV=consumers mise -C evaluation which pi, or
// claude, from the repository root). Their state lives under -state, or SPIKE_STATE_DIR, which
// defaults to the repository's untracked .state/: Pi's agent directory, sessions and cache under
// pi/, never the user's ~/.pi, and Claude Code's configuration directory at claude/, never the
// user's ~/.claude. Claude Code runs on the architect's subscription login, made once in that
// directory:
//
//	CLAUDE_CONFIG_DIR=$PWD/.state/claude MISE_ENV=consumers mise -C evaluation exec -- claude
//
// then /login. Its model requests go to the router, every model alias mapped to the chat model.
//
// It exits 0 when the run passes, 1 when it fails or can't run, and 2 on a usage error.
package concurrentstep

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
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/JaimeStill/spike-harness-driver/claude"
	"github.com/JaimeStill/spike-harness-driver/harness"
	"github.com/JaimeStill/spike-harness-driver/model"
	"github.com/JaimeStill/spike-harness-driver/pi"

	"github.com/JaimeStill/spike-model-hosting/evaluation/concurrent"
	"github.com/JaimeStill/spike-model-hosting/evaluation/consumers"
	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
	"github.com/JaimeStill/spike-model-hosting/hosting/router"
)

// maxTokens caps a model-client chat completion, reasoning included: room for a reasoning
// model's reasoning, such as gpt-oss-120b's, before a one-token answer.
const maxTokens = 4096

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

// errUsage marks an error in how the step was called, which exits 2 rather than 1.
var errUsage = errors.New("usage")

// usage is the step's usage, printed on -h and after a usage error.
const usage = `usage:
  evaluate concurrent [-router url] [-profile name|path] [-set name] [-rounds n] [-pi path] [-claude path] [-state dir] [-inject-swap]
`

// options are the step's flags, after the environment's fallbacks.
type options struct {
	router, profile, set, pi, claude, state string
	rounds                                  int
	timeout                                 time.Duration
	injectSwap                              bool
}

// Run runs the step with args, reading the environment through getenv, and returns its exit
// status.
func Run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
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
		_, _ = fmt.Fprintf(stderr, "evaluate concurrent: %v\n%s", err, usage)
		return 2
	default:
		_, _ = fmt.Fprintf(stderr, "evaluate concurrent: %v\n", err)
		return 1
	}
}

// parse reads the flags in args, each falling back to its environment variable.
func parse(args []string, getenv func(string) string) (options, error) {
	fs := flag.NewFlagSet("evaluate concurrent", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	var o options
	fs.StringVar(&o.router, "router", getenv("SPIKE_ROUTER_URL"), "the router's root URL (SPIKE_ROUTER_URL)")
	fs.StringVar(&o.profile, "profile", profile.HostStrixHalo, "the profile the router serves: a host class, found under the module root's devices/, or a TOML file's path")
	fs.StringVar(&o.set, "set", getenv("SPIKE_SET"), "the profile's set to run on (SPIKE_SET); empty is the startup set")
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
	p, _, err := profile.Open(o.profile)
	if err != nil {
		return concurrent.Report{}, err
	}
	set, err := p.Set(o.set)
	if err != nil {
		return concurrent.Report{}, fmt.Errorf("%w: %v", errUsage, err)
	}
	chatModel, embedModel := set.Roles.Chat, set.Roles.Embedding
	hc := &http.Client{}
	// Pi's provider can't carry autoload=false, so the run never starts against a model a request
	// would load.
	if err := router.CheckLoaded(ctx, hc, o.router, set.Models...); err != nil {
		return concurrent.Report{}, err
	}
	build, err := router.Build(ctx, hc, o.router)
	if err != nil {
		return concurrent.Report{}, err
	}
	piCmd, err := consumers.Executable(ctx, o.pi, "pi")
	if err != nil {
		return concurrent.Report{}, err
	}
	// Pi runs the chat model at its thinking level; a valid profile lists every role model.
	piDriver, err := newPiDriver(o, piCmd, chatModel, p.Model(chatModel).Consumers.Pi)
	if err != nil {
		return concurrent.Report{}, err
	}
	claudeCmd, err := consumers.Executable(ctx, o.claude, "claude")
	if err != nil {
		return concurrent.Report{}, err
	}
	claudeDriver, err := newClaudeDriver(o, claudeCmd, chatModel)
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

	// The chat streams share the chat model's own slots.
	slots := p.Model(chatModel).Slots
	workers := concurrent.SplitSlots(slots, 3)
	cfg := concurrent.Config{
		Rounds:         o.rounds,
		RequestTimeout: o.timeout,
		InjectSwap:     o.injectSwap,
		Chat: []concurrent.ChatStream{
			{Name: "pi", Workers: workers[0], Chatter: concurrent.HarnessChat{
				Driver: piDriver,
				Options: harness.Options{
					Provider: consumers.Provider, Model: chatModel,
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
	if _, err := fmt.Fprintf(stdout, "router %s, build %s, set %s\nchat %s on %d slots: pi %d, claude %d, model-chat %d workers, %d rounds\nembed %s: %d inputs, batch after batch until the chats finish, at least %d rounds\n\n",
		o.router, build, set.Name, chatModel, slots, workers[0], workers[1], workers[2], o.rounds, embedModel, len(inputs), o.rounds); err != nil {
		return concurrent.Report{}, err
	}
	rep, err := concurrent.Run(ctx, cfg)
	if err != nil {
		return rep, err
	}
	return rep, write(stdout, rep, build, set.Name)
}

// newPiDriver returns Pi's driver, its agent directory, sessions and cache under the state
// directory, on Pi's llama.cpp provider pointed at the router, at the chat model's thinking
// level (consumers.Pi).
func newPiDriver(o options, command, chatModel, thinking string) (pi.Driver, error) {
	root := filepath.Join(o.state, "pi")
	// Pi runs code from its agent directory and cache, so the driver refuses either unless it
	// is private to the user.
	for _, dir := range []string{root, filepath.Join(root, "work")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return pi.Driver{}, err
		}
	}
	return consumers.Pi(command, o.router, root, chatModel, thinking)
}

// newClaudeDriver returns Claude Code's driver, its configuration directory under the state
// directory, its model requests sent to the router as chatModel (consumers.ClaudeCode). It
// fails, naming the command that signs in, when that directory holds no login
// (consumers.CheckClaudeLogin), rather than start a run in which every Claude Code request fails.
func newClaudeDriver(o options, command, chatModel string) (claude.Driver, error) {
	dir := filepath.Join(o.state, "claude")
	for _, d := range []string{dir, filepath.Join(o.state, "claude-work")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return claude.Driver{}, err
		}
	}
	if err := consumers.CheckClaudeLogin(dir); err != nil {
		return claude.Driver{}, fmt.Errorf("claude code has no login in %s: %w", dir, err)
	}
	return consumers.ClaudeCode(command, dir, o.router, chatModel), nil
}

// write writes the report: one line per request, then the failures and the verdict, with the
// build and the set it ran on.
func write(w io.Writer, rep concurrent.Report, build, set string) error {
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
	_, err := fmt.Fprintf(w, "\n%s: %d chat requests, %d embeddings in %s, min embedding cosine %.6f, build %s, set %s\n",
		result, len(rep.Chats), len(rep.Embeds), round(rep.Elapsed), minCosine(rep), build, set)
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
