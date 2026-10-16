//go:build integration

package wire_test

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/JaimeStill/spike-harness-driver/claude"
	"github.com/JaimeStill/spike-harness-driver/harness"

	"github.com/JaimeStill/spike-model-hosting/evaluation/concurrent"
	"github.com/JaimeStill/spike-model-hosting/evaluation/consumers"
	"github.com/JaimeStill/spike-model-hosting/evaluation/wire"
	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
	"github.com/JaimeStill/spike-model-hosting/hosting/router"
)

// state is the repository's untracked state directory, where the recordings and the consumers'
// working directories go.
const state = "../../.state"

// strixHalo is the profile the router serves.
const strixHalo = "../../devices/strix-halo/profile.toml"

// liveRouter returns a Recorder in front of the router SPIKE_ROUTER_URL names, served on a
// local listener, that listener's URL, the roles of the profile's set SPIKE_SET names, or else
// of its startup set, and the consumer reasoning setting of the set's chat model. It skips the
// test when SPIKE_ROUTER_URL is unset, fails it unless the router has every model of the set
// loaded, and logs the build and the set it runs on.
func liveRouter(t *testing.T) (*wire.Recorder, string, profile.SetRoles, profile.ConsumerReasoning) {
	t.Helper()
	base := strings.TrimSuffix(os.Getenv("SPIKE_ROUTER_URL"), "/")
	if base == "" {
		t.Skip("SPIKE_ROUTER_URL is unset: set it to the router to run against")
	}
	p, err := profile.Load(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	set, err := p.Set(os.Getenv("SPIKE_SET"))
	if err != nil {
		t.Fatal(err)
	}
	// The consumers' providers can't carry autoload=false, so a test never starts against a
	// model a request would load.
	if err := router.CheckLoaded(t.Context(), http.DefaultClient, base, set.Models...); err != nil {
		t.Fatal(err)
	}
	build, err := router.Build(t.Context(), http.DefaultClient, base)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("build %s, set %s", build, set.Name)
	target, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	rec := wire.New(target)
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	return rec, srv.URL, set.Roles, p.Model(set.Roles.Chat).Consumers
}

// toolTurn writes a file holding a fresh code into a new working directory under state, and
// returns the directory, the prompt that asks the agent to read the file, and the code, which
// the agent can only answer with by calling its read tool.
func toolTurn(t *testing.T, consumer string) (dir, prompt, code string) {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	code = hex.EncodeToString(b)
	dir, err := filepath.Abs(filepath.Join(state, "wire", consumer+"-work"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("The code is "+code+".\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, "Read the file note.txt in the current directory with your file-reading tool, " +
		"then reply with the code it holds and nothing else.", code
}

// save writes rec's exchanges to state/wire/<consumer>-<time>.jsonl, credentials redacted, and
// logs the path.
func save(t *testing.T, rec *wire.Recorder, consumer string) {
	t.Helper()
	path := filepath.Join(state, "wire", consumer+"-"+time.Now().Format("20060102T150405")+".jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.WriteJSONL(f); err != nil {
		t.Error(err)
	}
	if err := f.Close(); err != nil {
		t.Error(err)
	}
	t.Logf("recorded %d exchanges to %s", len(rec.Exchanges()), path)
}

// send opens a session on d, sends prompt, and returns the exchange's text.
func send(t *testing.T, d harness.Driver, opts harness.Options, prompt string) string {
	t.Helper()
	text, err := concurrent.HarnessChat{Driver: d, Options: opts}.Chat(t.Context(), prompt)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

// message is a chat message in either API's request: Anthropic content blocks or OpenAI fields.
type message map[string]json.RawMessage

func (m message) role() string {
	var r string
	_ = json.Unmarshal(m["role"], &r)
	return r
}

// blocks returns an Anthropic message's content blocks; a string content has none.
func (m message) blocks() []map[string]any {
	var bs []map[string]any
	_ = json.Unmarshal(m["content"], &bs)
	return bs
}

func blockTypes(bs []map[string]any) []string {
	var out []string
	for _, b := range bs {
		t, _ := b["type"].(string)
		out = append(out, t)
	}
	return out
}

// TestClaudeThinkingRoundTrip runs one Claude Code turn that calls its Read tool, through a
// Recorder, from the run's own configuration directory on the subscription login, every model alias
// mapped to the set's chat model. It records the thinking request Claude Code sends, and checks
// thinking blocks stream back and the request after the tool call carries them back. SPIKE_CLAUDE
// names Claude Code (MISE_ENV=consumers mise -C evaluation which claude, from the repository
// root), and ../../.state/claude must hold its login.
func TestClaudeThinkingRoundTrip(t *testing.T) {
	rec, proxy, roles, _ := liveRouter(t)
	command := os.Getenv("SPIKE_CLAUDE")
	if command == "" {
		t.Skip("SPIKE_CLAUDE is unset: set it to Claude Code (MISE_ENV=consumers mise -C evaluation which claude)")
	}
	config, err := filepath.Abs(filepath.Join(state, "claude"))
	if err != nil {
		t.Fatal(err)
	}
	if err := consumers.CheckClaudeLogin(config); err != nil {
		t.Skipf("no Claude Code login in %s: %v", config, err)
	}
	dir, prompt, code := toolTurn(t, "claude")
	defer save(t, rec, "claude")
	text := send(t, consumers.ClaudeCode(command, config, proxy, roles.Chat), harness.Options{
		Provider: claude.Provider, Model: roles.Chat, Dir: dir, HarnessTools: []string{"Read"},
	}, prompt)
	t.Logf("answer: %q", text)
	if !strings.Contains(text, code) {
		t.Errorf("the answer %q doesn't carry the file's code %s", text, code)
	}

	var turns []wire.Exchange
	for _, x := range rec.Exchanges() {
		if strings.HasPrefix(x.Path, "/v1/messages") && !strings.Contains(x.Path, "count_tokens") {
			var req map[string]json.RawMessage
			if json.Unmarshal(x.Request, &req) == nil && len(req["tools"]) > 2 {
				turns = append(turns, x)
			}
		}
	}
	if len(turns) == 0 {
		t.Fatal("no /v1/messages request with tools reached the router through the Recorder")
	}

	// The thinking request: the first turn's parameters, without the prompt, the history and the
	// tools, and without metadata, which carries the account's user ID.
	var first map[string]json.RawMessage
	_ = json.Unmarshal(turns[0].Request, &first)
	for _, k := range []string{"system", "messages", "tools", "metadata"} {
		delete(first, k)
	}
	params, _ := json.Marshal(first)
	t.Logf("thinking request: POST %s %s", turns[0].Path, params)
	t.Logf("request headers: anthropic-beta %q, authorization %q", turns[0].Header.Get("Anthropic-Beta"), turns[0].Header.Get("Authorization"))

	streamed := false
	for _, x := range turns {
		for _, ev := range wire.Events(x.Response) {
			var e struct {
				Type  string `json:"type"`
				Block struct {
					Type string `json:"type"`
				} `json:"content_block"`
			}
			if json.Unmarshal([]byte(ev.Data), &e) == nil && e.Type == "content_block_start" && strings.Contains(e.Block.Type, "thinking") {
				streamed = true
			}
		}
	}
	t.Logf("thinking blocks streamed back: %v", streamed)
	if !streamed {
		t.Error("no response streamed a thinking block")
	}

	carried, toolTurns := false, 0
	for _, x := range turns {
		var req struct {
			Messages []message `json:"messages"`
		}
		_ = json.Unmarshal(x.Request, &req)
		for i, m := range req.Messages {
			if m.role() != "user" || !slices.Contains(blockTypes(m.blocks()), "tool_result") || i == 0 {
				continue
			}
			toolTurns++
			prev := blockTypes(req.Messages[i-1].blocks())
			t.Logf("after a tool call: the assistant message carries %v", prev)
			carried = carried || slices.Contains(prev, "thinking")
		}
	}
	if toolTurns == 0 {
		t.Fatal("no request carried a tool result: the model never called Read")
	}
	t.Logf("thinking carried back after a tool call: %v", carried)
	if !carried {
		t.Error("no request after a tool call carried the assistant's thinking back")
	}
}

// TestPiReasoningRoundTrip runs one Pi turn that calls its read tool, through a Recorder, at the
// set's chat model's thinking level, as the passk cell sets it, and reports whether the request
// after the tool call carries the assistant's reasoning back as reasoning_content. Pi's answer is a
// finding, not a pass or fail. SPIKE_PI names Pi (MISE_ENV=consumers mise -C evaluation which pi).
func TestPiReasoningRoundTrip(t *testing.T) {
	rec, proxy, roles, reasoning := liveRouter(t)
	command := os.Getenv("SPIKE_PI")
	if command == "" {
		t.Skip("SPIKE_PI is unset: set it to Pi (MISE_ENV=consumers mise -C evaluation which pi)")
	}
	dir, prompt, code := toolTurn(t, "pi")
	agent := t.TempDir()
	defer save(t, rec, "pi")
	d, err := consumers.Pi(command, proxy, agent, roles.Chat, reasoning.Pi)
	if err != nil {
		t.Fatal(err)
	}
	text := send(t, d, harness.Options{Provider: consumers.Provider, Model: roles.Chat, Dir: dir, HarnessTools: []string{"read"}}, prompt)
	t.Logf("answer: %q", text)
	if !strings.Contains(text, code) {
		t.Errorf("the answer %q doesn't carry the file's code %s", text, code)
	}

	if logReasoningSentBack(t, rec) == 0 {
		t.Fatal("no request carried a tool result: the model never called read")
	}
}

// TestOpenCodeReasoningRoundTrip runs one OpenCode turn that calls its read tool, through a
// Recorder, once with the set's chat model's interleaved setting naming reasoning_content and its
// options, as the passk cell sets them, and once without the file that holds both, and reports
// whether the request after the tool call carries the assistant's reasoning back. OpenCode's answer
// is a finding, not a pass or fail. SPIKE_OPENCODE names OpenCode (MISE_ENV=consumers mise -C
// evaluation which opencode).
func TestOpenCodeReasoningRoundTrip(t *testing.T) {
	command := os.Getenv("SPIKE_OPENCODE")
	if command == "" {
		t.Skip("SPIKE_OPENCODE is unset: set it to OpenCode (MISE_ENV=consumers mise -C evaluation which opencode)")
	}
	for _, interleaved := range []bool{true, false} {
		name := "interleaved"
		if !interleaved {
			name = "unset"
		}
		t.Run(name, func(t *testing.T) {
			rec, proxy, roles, reasoning := liveRouter(t)
			consumer := "opencode-" + name
			dir, prompt, code := toolTurn(t, consumer)
			var images []string
			if roles.Vision != "" {
				images = append(images, roles.Vision)
			}
			d, err := consumers.OpenCode(command, proxy, filepath.Join(t.TempDir(), "opencode"), roles.Chat, reasoning.OpenCode, images...)
			if err != nil {
				t.Fatal(err)
			}
			if !interleaved {
				d.Env = nil
			}
			defer save(t, rec, consumer)
			text := send(t, d, harness.Options{Provider: consumers.Provider, Model: roles.Chat, Dir: dir, HarnessTools: []string{"read"}}, prompt)
			t.Logf("answer: %q", text)
			if !strings.Contains(text, code) {
				t.Errorf("the answer %q doesn't carry the file's code %s", text, code)
			}
			if n := logReasoningSentBack(t, rec); n == 0 {
				t.Fatal("no request carried a tool result: the model never called read")
			}
		})
	}
}

// logReasoningSentBack logs, for each OpenAI chat request rec holds that carries a tool result,
// the fields of the assistant message before it and whether reasoning_content went back, and
// returns how many tool results it found.
func logReasoningSentBack(t *testing.T, rec *wire.Recorder) int {
	t.Helper()
	toolTurns := 0
	for _, x := range rec.Exchanges() {
		if !strings.HasPrefix(x.Path, "/v1/chat/completions") {
			continue
		}
		var req struct {
			Messages []message `json:"messages"`
		}
		_ = json.Unmarshal(x.Request, &req)
		for i, m := range req.Messages {
			if m.role() != "tool" || i == 0 || req.Messages[i-1].role() != "assistant" {
				continue
			}
			toolTurns++
			prev := req.Messages[i-1]
			keys := make([]string, 0, len(prev))
			for k := range prev {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			var reasoning string
			_ = json.Unmarshal(prev["reasoning_content"], &reasoning)
			t.Logf("after a tool call: the assistant message has fields %v; reasoning_content sent back: %v (%d chars)",
				keys, reasoning != "", len(reasoning))
		}
	}
	return toolTurns
}
