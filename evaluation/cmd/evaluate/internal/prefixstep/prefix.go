// Package prefixstep is evaluate's prefix step, which measures prefix-cache reuse on the router:
// for each generative model of a set, a run of requests that share one long prefix and end in
// different questions.
//
// Usage:
//
//	evaluate prefix [-router url] [-profile name|path] [-set name] [-prefix-tokens n] [-requests n] [-max-tokens n] [-slot id]
//
// The set is the profile's set -set names, or SPIKE_SET, or else its startup set. For each of
// the set's generative models, the step builds a prefix of exactly
// -prefix-tokens tokens in that model's own tokenizer, through the router's /tokenize and
// /detokenize, and starts it with a nonce unique to the run, so no cache on the server holds any
// of it before the first request. It then sends -requests streamed chat completions back to back,
// each the prefix followed by a different question, and prints, for each, llama.cpp's own
// accounting from the response's timings (cache_n, the prompt tokens served from the cache, and
// prompt_n, the ones it evaluated, with the time it took) beside the client's time to first
// token and to the last byte. The first request is the cold one; every later one should find the
// prefix cached.
//
// Requests go unpinned by default, as a consumer sends them: the server picks the slot, by
// prompt similarity. -slot pins every request to one slot (llama.cpp's id_slot), which takes the
// server's slot choice out of what is measured.
//
// The router comes from -router or SPIKE_ROUTER_URL. Every model of the set must be loaded
// already: the step checks GET /models first, and every request carries autoload=false, so it
// never loads one. The output names the build and the set it ran on.
//
// It exits 0 when every request was measured, 1 when one failed or the step can't run, and 2 on
// a usage error.
package prefixstep

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
	"github.com/JaimeStill/spike-model-hosting/hosting/router"
)

// questions end the prefix, one per request, in turn: each differs from the next from its first
// token, so the shared part of two prompts is the prefix and the template around it.
var questions = []string{
	"In one sentence, what is the passage above about?",
	"Name one place the passage above mentions.",
	"Which paragraph above is the longest? Answer with its number.",
	"Give a title for the passage above in five words or fewer.",
	"Does the passage above mention a river? Answer yes or no.",
	"Quote the first sentence of paragraph 3 above.",
	"How many paragraphs are above? Answer with a number.",
	"Summarize the last paragraph above in one sentence.",
}

// The words the filler text is made of: varied enough that it reads as text to a tokenizer.
var (
	subjects = []string{"The harbor master", "A surveyor", "The night clerk", "An old cartographer", "The ferry pilot", "A visiting botanist", "The archivist", "A railway engineer"}
	verbs    = []string{"recorded", "measured", "described", "sketched", "questioned", "catalogued", "mapped", "revisited"}
	objects  = []string{"the tide tables", "the northern ridge", "a flooded quarry", "the lighthouse stairs", "the river delta", "a ruined mill", "the salt flats", "the signal tower"}
	times    = []string{"before dawn", "in late autumn", "during the storm", "after the census", "at the spring fair", "over three winters", "on the last ferry", "beneath a red moon"}
)

// errUsage marks an error in how the step was called, which exits 2 rather than 1.
var errUsage = errors.New("usage")

// usage is the step's usage, printed on -h and after a usage error.
const usage = `usage:
  evaluate prefix [-router url] [-profile name|path] [-set name] [-prefix-tokens n] [-requests n] [-max-tokens n] [-slot id]
`

// options are the step's flags, after the environment's fallbacks.
type options struct {
	router, profile, set                    string
	prefixTokens, requests, maxTokens, slot int
	timeout                                 time.Duration
}

// Run runs the step with args, reading the environment through getenv, and returns its exit
// status.
func Run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	opts, err := parse(args, getenv)
	if err == nil {
		err = measure(ctx, opts, stdout)
		if err == nil {
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
		_, _ = fmt.Fprintf(stderr, "evaluate prefix: %v\n%s", err, usage)
		return 2
	default:
		_, _ = fmt.Fprintf(stderr, "evaluate prefix: %v\n", err)
		return 1
	}
}

// parse reads the flags in args, the router and the set falling back to SPIKE_ROUTER_URL and
// SPIKE_SET.
func parse(args []string, getenv func(string) string) (options, error) {
	fs := flag.NewFlagSet("evaluate prefix", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	var o options
	fs.StringVar(&o.router, "router", getenv("SPIKE_ROUTER_URL"), "the router's root URL (SPIKE_ROUTER_URL)")
	fs.StringVar(&o.profile, "profile", profile.HostStrixHalo, "the profile the router serves: a host class, found under the module root's devices/, or a TOML file's path")
	fs.StringVar(&o.set, "set", getenv("SPIKE_SET"), "the profile's set to measure (SPIKE_SET); empty is the startup set")
	fs.IntVar(&o.prefixTokens, "prefix-tokens", 8192, "the shared prefix's length, in the model's tokens")
	fs.IntVar(&o.requests, "requests", 5, "the requests per model, the first of them cold")
	fs.IntVar(&o.maxTokens, "max-tokens", 32, "the tokens each request may generate")
	fs.IntVar(&o.slot, "slot", -1, "the slot to pin every request to; -1 lets the server choose")
	fs.DurationVar(&o.timeout, "timeout", 5*time.Minute, "the bound on one request")
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
	case o.prefixTokens < 1:
		return o, fmt.Errorf("%w: -prefix-tokens must be at least 1", errUsage)
	case o.requests < 2:
		return o, fmt.Errorf("%w: -requests must be at least 2, a cold one and a warm one", errUsage)
	case o.maxTokens < 1:
		return o, fmt.Errorf("%w: -max-tokens must be at least 1", errUsage)
	case o.slot < -1:
		return o, fmt.Errorf("%w: -slot must be a slot id, or -1", errUsage)
	}
	o.router = strings.TrimSuffix(o.router, "/")
	return o, nil
}

// result is one request's measurement.
type result struct {
	// TTFT is the time to the first streamed token, reasoning included; Total, to the end of
	// the stream.
	TTFT, Total time.Duration
	// CacheN, PromptN and PromptMS are the server's: the prompt tokens served from the cache, the
	// ones evaluated, and the milliseconds evaluating them took.
	CacheN, PromptN int
	PromptMS        float64
}

// measure runs every generative model's requests and writes the table.
func measure(ctx context.Context, o options, stdout io.Writer) error {
	p, _, err := profile.Open(o.profile)
	if err != nil {
		return err
	}
	set, err := p.Set(o.set)
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	var ids []string
	for _, id := range set.Models {
		if p.Model(id).Kind == profile.KindGenerative {
			ids = append(ids, id)
		}
	}
	hc := &http.Client{}
	if err := router.CheckLoaded(ctx, hc, o.router, set.Models...); err != nil {
		return err
	}
	build, err := router.Build(ctx, hc, o.router)
	if err != nil {
		return err
	}
	slot := "unpinned, the server's choice"
	if o.slot >= 0 {
		slot = fmt.Sprintf("pinned to slot %d", o.slot)
	}
	if _, err := fmt.Fprintf(stdout, "router %s, build %s, set %s\nprefix %d tokens, %d requests per model, %d tokens out, %s\n\n",
		o.router, build, set.Name, o.prefixTokens, o.requests, o.maxTokens, slot); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	_, _ = fmt.Fprintln(tw, "MODEL\tREQUEST\tCACHE_N\tPROMPT_N\tPROMPT_MS\tTTFT_MS\tTOTAL_MS\t")
	for _, id := range ids {
		prefix, err := buildPrefix(ctx, hc, o.router, id, o.prefixTokens)
		if err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
		for i := range o.requests {
			r, err := complete(ctx, hc, o, id, prefix+"\n\n"+questions[i%len(questions)])
			if err != nil {
				_ = tw.Flush()
				return fmt.Errorf("%s request %d: %w", id, i+1, err)
			}
			_, _ = fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%.0f\t%d\t%d\t\n",
				id, i+1, r.CacheN, r.PromptN, r.PromptMS, r.TTFT.Milliseconds(), r.Total.Milliseconds())
		}
	}
	return tw.Flush()
}

// buildPrefix returns a prefix exactly n of the model's tokens long, its first words a nonce
// unique to the run, cut from filler text with the router's /tokenize and /detokenize.
func buildPrefix(ctx context.Context, hc *http.Client, base, id string, n int) (string, error) {
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	var text strings.Builder
	fmt.Fprintf(&text, "Run %s. Read the passage below, then answer the question after it.\n\n", hex.EncodeToString(nonce))
	// Each paragraph is about a hundred tokens, so n/50 paragraphs leave the cut room to spare.
	for i := range n/50 + 1 {
		fmt.Fprintf(&text, "Paragraph %d. ", i+1)
		for j := range 5 {
			k := i*5 + j
			fmt.Fprintf(&text, "%s %s %s %s, and wrote it down in ledger %d. ",
				subjects[k%len(subjects)], verbs[(k/3)%len(verbs)], objects[(k/7)%len(objects)], times[(k/11)%len(times)], k+1)
		}
		text.WriteString("\n\n")
	}
	var tok struct {
		Tokens []int `json:"tokens"`
	}
	if err := post(ctx, hc, base+"/tokenize?autoload=false",
		map[string]any{"model": id, "content": text.String()}, &tok); err != nil {
		return "", err
	}
	if len(tok.Tokens) < n {
		return "", fmt.Errorf("the filler text is %d tokens, short of the %d the prefix needs", len(tok.Tokens), n)
	}
	var detok struct {
		Content string `json:"content"`
	}
	if err := post(ctx, hc, base+"/detokenize?autoload=false",
		map[string]any{"model": id, "tokens": tok.Tokens[:n]}, &detok); err != nil {
		return "", err
	}
	return detok.Content, nil
}

// chunk is the part of a streamed chat completion chunk the step reads; llama.cpp adds its
// timings to the last one.
type chunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
	} `json:"choices"`
	Timings *struct {
		CacheN   int     `json:"cache_n"`
		PromptN  int     `json:"prompt_n"`
		PromptMS float64 `json:"prompt_ms"`
	} `json:"timings"`
}

// complete sends one streamed chat completion and measures it.
func complete(ctx context.Context, hc *http.Client, o options, id, prompt string) (result, error) {
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	body := map[string]any{
		"model":      id,
		"stream":     true,
		"max_tokens": o.maxTokens,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	}
	if o.slot >= 0 {
		body["id_slot"] = o.slot
	}
	data, err := json.Marshal(body)
	if err != nil {
		return result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.router+"/v1/chat/completions?autoload=false", bytes.NewReader(data))
	if err != nil {
		return result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := hc.Do(req)
	if err != nil {
		return result{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return result{}, fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(msg))
	}
	var r result
	timed := false
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		payload, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok || payload == "[DONE]" {
			continue
		}
		var c chunk
		if err := json.Unmarshal([]byte(payload), &c); err != nil {
			return result{}, fmt.Errorf("a stream chunk: %w", err)
		}
		if r.TTFT == 0 {
			for _, ch := range c.Choices {
				if ch.Delta.Content != "" || ch.Delta.ReasoningContent != "" {
					r.TTFT = time.Since(start)
				}
			}
		}
		if c.Timings != nil {
			r.CacheN, r.PromptN, r.PromptMS = c.Timings.CacheN, c.Timings.PromptN, c.Timings.PromptMS
			timed = true
		}
	}
	if err := sc.Err(); err != nil {
		return result{}, err
	}
	r.Total = time.Since(start)
	switch {
	case !timed:
		return result{}, errors.New("the stream carried no timings")
	case r.TTFT == 0:
		return result{}, errors.New("the stream carried no token")
	}
	return r, nil
}

// post sends in as a JSON POST to u, and decodes the JSON answer into out.
func post(ctx context.Context, hc *http.Client, u string, in, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST %s: %s", u, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("POST %s: %w", u, err)
	}
	return nil
}
