package gate_test

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"maps"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JaimeStill/spike-harness-driver/clutch/conform"

	"github.com/JaimeStill/spike-model-hosting/evaluation/gate"
	"github.com/JaimeStill/spike-model-hosting/evaluation/passk"
	"github.com/JaimeStill/spike-model-hosting/evaluation/wire"
)

// fakeRouter is an httptest llama.cpp router: it lists models with their statuses, answers
// chat completions with the timings it is given, tool calls with get_weather, and embeddings
// with a vector derived from each input, and records each request it serves.
type fakeRouter struct {
	srv    *httptest.Server
	status map[string]string
	build  string
	// decode and prefill are every chat completion's predicted_per_second and prompt_per_second.
	decode, prefill float64
	// crossed is a model whose nonce answers carry crossedNonce in place of their own, as an
	// answer that reached the wrong request would.
	crossed string
	// empty is a model whose nonce answers come back empty.
	empty string
	// cutOff is a model whose nonce answers hit their max_tokens: a few words, then
	// finish_reason "length", the nonce never reached unless crossed puts a foreign one first.
	cutOff string
	// refuse is a model whose chat completions and embeddings fail with a server error.
	refuse string
	// slowTimed delays each timed request after the first, fitness's: each image question (the
	// probe's "What shapes" question) and each lone 32-word embedding, so capacity's N of them at
	// once answer slower than fitness's one.
	slowTimed time.Duration
	timedSeen int
	// exhaust is a model whose nonce requests fail as llama.cpp's server fails a prompt longer
	// than its slot's context: 400, exceed_context_size_error.
	exhaust string
	// unloadOnToolCall is a model the router lists unloaded from the first tool call on, fitness's
	// first request, after the run's reload and warm-up.
	unloadOnToolCall string

	mu       sync.Mutex
	requests []string
	// prefixes are the long prompts' first words seen, which a later prompt starting the same way
	// finds cached, as llama.cpp's prompt cache would.
	prefixes map[string]bool
	// vocab and words are the fake tokenizer's vocabulary: each word's ID, and each ID's word.
	vocab map[string]int
	words []string
	// restarting is set while a fake restart has the router down.
	restarting bool
	// audio are the chat completions' bodies that carried an audio clip, in order; nonced, the
	// bodies of the chat completions that carried a nonce, by model.
	audio  []map[string]any
	nonced map[string][]map[string]any
	// images are the image parts' data URLs, in order; longest is each model's longest chat
	// prompt, and widest each model's largest embedding input, in words (the fake's tokens).
	images  []string
	longest map[string]int
	widest  map[string]int

	// presets, when set, is the router's presets file: GET /models?reload=1 reads it again,
	// unloading each loaded model whose section changed since it loaded, and a load takes its
	// model's section's parallel and c, which GET /props?model= reports as total_slots and
	// n_ctx. sections are the sections each loaded model runs at; served, its slots and c.
	presets  string
	sections map[string]map[string]string
	served   map[string]gate.Setting
	// failReload is the GET /models?reload=1, counted from 1, that fails; reloads, those seen.
	failReload, reloads int
	// decodeAt, when set, is a nonce request's decode at its model's served slots; slowAt, at
	// served slots it holds, the decode of the first nonce request served at them, the rest at
	// decodeAt.
	decodeAt func(slots int) float64
	slowAt   map[int]float64
	slowSeen map[int]bool
	// exhaustAt says a model's nonce requests run out of context at its served slots.
	exhaustAt func(model string, slots int) bool
}

var fakeNonce = regexp.MustCompile(`nonce-[0-9a-f]{16}`)

// crossedNonce is the nonce a crossed model's answers carry, one no request sent.
const crossedNonce = "nonce-0123456789abcdef"

func newFakeRouter(t *testing.T, models ...string) *fakeRouter {
	f := &fakeRouter{status: map[string]string{}, prefixes: map[string]bool{}, vocab: map[string]int{}, build: "b11669-620d2225c", decode: 50, prefill: 700,
		longest: map[string]int{}, widest: map[string]int{}, nonced: map[string][]map[string]any{}}
	for _, m := range models {
		f.status[m] = "loaded"
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

// count returns how many recorded requests start with prefix.
func (f *fakeRouter) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if strings.HasPrefix(r, prefix) {
			n++
		}
	}
	return n
}

// metrics answers GET /metrics for a model with llama.cpp's counters, each grown by a fixed step
// a request: 1000 prompt tokens over 2 seconds, 300 predicted over 6, 50 cached.
func (f *fakeRouter) metrics(w http.ResponseWriter, q url.Values) {
	if q.Get("autoload") != "false" {
		http.Error(w, "a model-routed request without autoload=false", http.StatusBadRequest)
		return
	}
	f.record("metrics " + q.Get("model"))
	n := float64(f.count("metrics " + q.Get("model")))
	for _, c := range []struct {
		name string
		step float64
	}{{"prompt_tokens_total", 1000}, {"prompt_seconds_total", 2}, {"tokens_predicted_total", 300},
		{"tokens_predicted_seconds_total", 6}, {"prompt_tokens_cached_total", 50}} {
		_, _ = fmt.Fprintf(w, "# TYPE llamacpp:%s counter\nllamacpp:%s %g\n", c.name, c.name, c.step*n)
	}
}

// restart takes the router down, as a restart does, then brings it back with each model listed
// unloaded, then loaded.
func (f *fakeRouter) restart(pause time.Duration) {
	f.mu.Lock()
	f.restarting = true
	f.mu.Unlock()
	time.Sleep(pause)
	f.mu.Lock()
	f.restarting = false
	saved := maps.Clone(f.status)
	for m := range f.status {
		f.status[m] = "unloaded"
	}
	f.mu.Unlock()
	time.Sleep(pause)
	f.mu.Lock()
	maps.Copy(f.status, saved)
	f.mu.Unlock()
}

// down reports whether a fake restart has the router down.
func (f *fakeRouter) down() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.restarting
}

func (f *fakeRouter) record(s string) {
	f.mu.Lock()
	f.requests = append(f.requests, s)
	f.mu.Unlock()
}

func (f *fakeRouter) serve(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		m, _ := body["model"].(string)
		// A load or unload takes effect at once, and is recorded as "load <model>" or
		// "unload <model>".
		if verb, ok := strings.CutPrefix(r.URL.Path, "/models/"); ok && (verb == "load" || verb == "unload") {
			f.mu.Lock()
			f.requests = append(f.requests, verb+" "+m)
			f.status[m] = verb + "ed"
			if verb == "load" && f.presets != "" {
				f.apply(m)
			}
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"success":true}`))
			return
		}
		if r.URL.Query().Get("autoload") != "false" {
			http.Error(w, "a model-routed request without autoload=false", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		status := f.status[m]
		f.mu.Unlock()
		if status != "loaded" {
			http.Error(w, "model not loaded: "+m, http.StatusBadRequest)
			return
		} else if m != "" && m == f.refuse {
			f.record("refused " + m)
			http.Error(w, "refused: "+m, http.StatusInternalServerError)
			return
		}
	}
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch r.URL.Path {
	case "/models":
		if r.URL.Query().Get("reload") == "1" {
			if err := f.reread(); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		f.record("models")
		if f.down() {
			http.Error(w, "restarting", http.StatusServiceUnavailable)
			return
		}
		var data []map[string]any
		f.mu.Lock()
		for id, s := range f.status {
			data = append(data, map[string]any{"id": id, "status": map[string]any{"value": s}})
		}
		f.mu.Unlock()
		reply(map[string]any{"data": data})
	case "/props":
		f.record("props")
		props := map[string]any{"build_info": f.build}
		if m := r.URL.Query().Get("model"); m != "" {
			f.mu.Lock()
			at, ok := f.served[m]
			status := f.status[m]
			f.mu.Unlock()
			if !ok || status != "loaded" {
				http.Error(w, "model not loaded: "+m, http.StatusBadRequest)
				return
			}
			props["total_slots"], props["default_generation_settings"] = at.Slots, map[string]any{"n_ctx": at.C}
		}
		reply(props)
	case "/v1/messages":
		f.record("messages")
		if f.unloadOnToolCall != "" {
			f.mu.Lock()
			f.status[f.unloadOnToolCall] = "unloaded"
			f.mu.Unlock()
		}
		reply(map[string]any{"content": []map[string]any{{"type": "tool_use", "name": "get_weather", "input": map[string]any{"city": "Paris"}}}})
	case "/metrics":
		f.metrics(w, r.URL.Query())
	case "/tokenize":
		// A word is a token, its ID its place in the fake's vocabulary, so text detokenizes back
		// to its own words, a run's nonce among them.
		content, _ := body["content"].(string)
		var toks []int
		f.mu.Lock()
		for w := range strings.FieldsSeq(content) {
			id, ok := f.vocab[w]
			if !ok {
				id = len(f.words)
				f.vocab[w], f.words = id, append(f.words, w)
			}
			toks = append(toks, id)
		}
		f.mu.Unlock()
		reply(map[string]any{"tokens": toks})
	case "/detokenize":
		toks, _ := body["tokens"].([]any)
		words := make([]string, len(toks))
		f.mu.Lock()
		for i, id := range toks {
			words[i] = f.words[int(id.(float64))]
		}
		f.mu.Unlock()
		reply(map[string]any{"content": strings.Join(words, " ")})
	case "/v1/embeddings":
		f.embeddings(body, reply)
	case "/v1/chat/completions":
		f.chat(w, body, reply)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeRouter) chat(w http.ResponseWriter, body map[string]any, reply func(any)) {
	msgs, _ := body["messages"].([]any)
	content := msgs[0].(map[string]any)["content"]
	var text strings.Builder
	image, audio := false, false
	switch c := content.(type) {
	case string:
		text.WriteString(c)
	case []any:
		for _, p := range c {
			part := p.(map[string]any)
			switch part["type"] {
			case "text":
				text.WriteString(part["text"].(string))
			case "image_url":
				image = true
				f.mu.Lock()
				f.images = append(f.images, part["image_url"].(map[string]any)["url"].(string))
				f.mu.Unlock()
			case "input_audio":
				audio = true
			}
		}
	}
	if strings.Contains(text.String(), "What shapes are in this image") {
		f.timed()
	}
	answer := "ready"
	switch {
	case image:
		answer = "A red circle, a blue square, and a green triangle."
	case audio:
		answer = "The access code is seven four two nine."
	}
	kind := "chat"
	if n := fakeNonce.FindString(text.String()); n != "" {
		m, _ := body["model"].(string)
		f.mu.Lock()
		f.nonced[m] = append(f.nonced[m], body)
		f.mu.Unlock()
		f.mu.Lock()
		slots := f.served[m].Slots
		f.mu.Unlock()
		if m == f.exhaust || (f.exhaustAt != nil && f.exhaustAt(m, slots)) {
			f.record("exhausted " + m)
			w.WriteHeader(http.StatusBadRequest)
			reply(map[string]any{"error": map[string]any{"code": 400, "type": "exceed_context_size_error",
				"message": "the request exceeds the available context size, try increasing it"}})
			return
		}
		if body["model"] == f.crossed {
			n = crossedNonce
		}
		kind, answer = "capacity", answer+"\n"+n
		if body["model"] == f.empty {
			answer = ""
		}
	}
	finish := "stop"
	if kind == "capacity" && body["model"] == f.cutOff {
		finish, answer = "length", "The image shows"
		if body["model"] == f.crossed {
			answer = crossedNonce + " The image shows"
		}
	}
	if audio {
		f.mu.Lock()
		f.audio = append(f.audio, body)
		f.mu.Unlock()
	}
	f.mu.Lock()
	m, _ := body["model"].(string)
	f.longest[m] = max(f.longest[m], len(strings.Fields(text.String())))
	f.mu.Unlock()
	stream, _ := body["stream"].(bool)
	f.record(fmt.Sprintf("%s %v %s", kind, stream, body["model"]))
	timings := map[string]any{
		"prompt_n": len(strings.Fields(text.String())), "prompt_per_second": f.prefill,
		"predicted_n": 10, "predicted_per_second": f.decodeOf(kind == "capacity", m), "prompt_ms": 120.0,
	}
	// A long prompt whose start was sent before finds all but its last ten words cached.
	if words := strings.Fields(text.String()); len(words) > 1000 {
		key := fmt.Sprint(body["model"], strings.Join(words[:1000], " "))
		f.mu.Lock()
		if f.prefixes[key] {
			timings["cache_n"], timings["prompt_n"] = len(words)-10, 10
		}
		f.prefixes[key] = true
		f.mu.Unlock()
	}
	if !stream {
		reply(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": answer}, "finish_reason": finish}},
			"timings": timings,
		})
		return
	}
	for _, chunk := range []map[string]any{
		{"choices": []map[string]any{{"delta": map[string]any{"content": answer}}}},
		{"choices": []map[string]any{}, "timings": timings},
	} {
		b, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

// timed counts a timed request and, after the first, waits slowTimed.
func (f *fakeRouter) timed() {
	f.mu.Lock()
	f.timedSeen++
	n := f.timedSeen
	f.mu.Unlock()
	if n > 1 {
		time.Sleep(f.slowTimed)
	}
}

func (f *fakeRouter) embeddings(body map[string]any, reply func(any)) {
	f.record("embeddings")
	var inputs []string
	switch in := body["input"].(type) {
	case string:
		inputs = []string{in}
	case []any:
		for _, s := range in {
			inputs = append(inputs, s.(string))
		}
	}
	if len(inputs) == 1 && len(strings.Fields(inputs[0])) == 32 {
		f.timed()
	}
	var data []map[string]any
	tokens := 0
	m, _ := body["model"].(string)
	for i, in := range inputs {
		tokens += len(strings.Fields(in))
		f.mu.Lock()
		f.widest[m] = max(f.widest[m], len(strings.Fields(in)))
		f.mu.Unlock()
		h := fnv.New64a()
		_, _ = h.Write([]byte(in))
		rng := rand.New(rand.NewPCG(h.Sum64(), 0))
		v := make([]float32, 768)
		for j := range v {
			v[j] = float32(rng.NormFloat64())
		}
		data = append(data, map[string]any{"index": i, "embedding": v})
	}
	reply(map[string]any{"data": data, "usage": map[string]any{"prompt_tokens": tokens}})
}

// fakeFootprint returns the pool's free memory from free, one reading after another, the last
// repeated, out of a 98304MiB pool, with system RAM beside it: rssMiB for each of set A's models
// and 1000 more of RAM used for each MiB less free.
type fakeFootprint struct {
	mu   sync.Mutex
	free []int
	n    int
	err  error
}

// rssMiB is every model process's resident memory in fakeFootprint's readings.
const rssMiB = 400

func (f *fakeFootprint) Read(context.Context) (gate.Pool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return gate.Pool{}, f.err
	}
	free := f.free[min(f.n, len(f.free)-1)]
	f.n++
	rss := map[string]int{}
	for _, m := range []string{gptOSS, gemma26B, gemmaE4B, embedding} {
		rss[m] = rssMiB
	}
	ram := &gate.RAM{UsedMiB: 100000 - free, AvailableMiB: 28000 + free, TotalMiB: 128076, RSSMiB: rss}
	return gate.Pool{UsedMiB: 98304 - free, TotalMiB: 98304, RAM: ram}, nil
}

// fakeConform answers the conform tier with recorded trials, k a harness, and records each plan
// it is given. With the plan's Drop it ends each harness's trials at its first failed one
// (passk.Judge), as passk.Runner does, so a harness's trials past that never count.
type fakeConform struct {
	trials map[string][]passk.Trial
	err    error
	plans  []passk.Plan
}

// newFakeConform returns a fakeConform over k trials in which Pi passes every capability,
// Claude Code fails tool and skill and is n/a on dropped-image, and OpenCode fails skill with
// two invalid requests in its first trial, one in its second, and passes after: Pi fits, and
// Claude Code and OpenCode drop at their first trial.
func newFakeConform(k int) *fakeConform {
	claude := conformReport("claude", map[string]conform.Status{"tool": conform.Fail, "skill": conform.Fail, "dropped-image": conform.NA})
	f := &fakeConform{trials: map[string][]passk.Trial{}}
	for i := range k {
		f.trials["pi"] = append(f.trials["pi"], passk.Trial{Report: conformReport("pi", nil)})
		f.trials["claude"] = append(f.trials["claude"], passk.Trial{Report: claude})
		t := passk.Trial{Report: conformReport("opencode", nil)}
		if i < 2 {
			t.Report = conformReport("opencode", map[string]conform.Status{"skill": conform.Fail})
			// Two invalid requests in the first trial, one in the second.
			for range 2 - i {
				t.Invalid = append(t.Invalid, wire.Exchange{Method: "POST", Path: "/v1/chat/completions", Status: 500})
			}
		}
		f.trials["opencode"] = append(f.trials["opencode"], t)
	}
	return f
}

// conformReport is a consumer's trial in which every capability passed except those in
// statuses.
func conformReport(consumer string, statuses map[string]conform.Status) conform.Report {
	rep := conform.Report{Harness: consumer, Provider: "llama.cpp", Version: consumer + " 1.0.0"}
	for _, c := range conform.Capabilities {
		s, ok := statuses[c.Name]
		if !ok {
			s = conform.Pass
		}
		rep.Results = append(rep.Results, conform.Result{Capability: c.Name, Status: s, Reason: c.Name + " " + s.String()})
	}
	return rep
}

func (f *fakeConform) Run(_ context.Context, plan passk.Plan) (passk.Result, error) {
	f.plans = append(f.plans, plan)
	if f.err != nil {
		return passk.Result{}, f.err
	}
	res := passk.Result{Trials: map[string][]passk.Trial{}, Wall: map[string]time.Duration{}}
	for _, h := range plan.Harnesses {
		ts := f.trials[h.Name]
		if plan.Drop {
			if fit := passk.Judge(h.Name, ts, plan.K); fit.Failed() {
				ts = ts[:fit.Trial]
			}
		}
		res.Trials[h.Name] = ts
		res.Wall[h.Name] = time.Duration(len(ts)) * time.Minute
		fit := passk.Judge(h.Name, ts, plan.K)
		res.Rows, res.Fits = append(res.Rows, fit.Row), append(res.Fits, fit)
	}
	return res, nil
}

// decodeOf is a chat completion's decode: decodeAt's at the model's served slots for a nonce
// request, slowAt's for the first nonce request at slots it holds, and decode otherwise.
func (f *fakeRouter) decodeOf(nonced bool, model string) float64 {
	if !nonced || f.decodeAt == nil {
		return f.decode
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	slots := f.served[model].Slots
	if v, ok := f.slowAt[slots]; ok && !f.slowSeen[slots] {
		if f.slowSeen == nil {
			f.slowSeen = map[int]bool{}
		}
		f.slowSeen[slots] = true
		return v
	}
	return f.decodeAt(slots)
}

// readPresets parses the presets file into its sections' keys, comments left out.
func (f *fakeRouter) readPresets() (map[string]map[string]string, error) {
	b, err := os.ReadFile(f.presets)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]string{}
	var cur map[string]string
	for line := range strings.Lines(string(b)) {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || strings.HasPrefix(line, ";"):
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			cur = map[string]string{}
			out[line[1:len(line)-1]] = cur
		default:
			k, v, _ := strings.Cut(line, "=")
			if cur != nil {
				cur[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
	}
	return out, nil
}

// apply has model run at its section of the presets file, as a load does; f.mu is held.
func (f *fakeRouter) apply(model string) {
	secs, err := f.readPresets()
	if err != nil {
		return
	}
	slots, _ := strconv.Atoi(secs[model]["parallel"])
	c, _ := strconv.Atoi(secs[model]["c"])
	f.sections[model], f.served[model] = secs[model], gate.Setting{Slots: slots, C: c}
	f.requests = append(f.requests, fmt.Sprintf("served %s %d %d", model, slots, c))
}

// reread reads the presets file again, as GET /models?reload=1 does: each loaded model whose
// section changed since it loaded is unloaded ("reread-unload <model>"); none is loaded.
func (f *fakeRouter) reread() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reloads++
	if f.reloads == f.failReload {
		return fmt.Errorf("fake: reload %d fails", f.reloads)
	}
	secs, err := f.readPresets()
	if err != nil {
		return err
	}
	f.requests = append(f.requests, "reread")
	for m, s := range f.status {
		if s == "loaded" && !maps.Equal(secs[m], f.sections[m]) {
			f.status[m] = "unloaded"
			f.requests = append(f.requests, "reread-unload "+m)
		}
	}
	return nil
}

// usePresets makes path the router's presets file, holding data, and has every loaded model run
// at its section.
func (f *fakeRouter) usePresets(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.presets, f.sections, f.served = path, map[string]map[string]string{}, map[string]gate.Setting{}
	for m, s := range f.status {
		if s == "loaded" {
			f.apply(m)
		}
	}
}
