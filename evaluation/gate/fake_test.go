package gate_test

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/evaluation/gate"
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

	mu       sync.Mutex
	requests []string
	// audio are the chat completions' bodies that carried an audio clip, in order.
	audio []map[string]any
}

var fakeNonce = regexp.MustCompile(`nonce-[0-9a-f]{16}`)

func newFakeRouter(t *testing.T, models ...string) *fakeRouter {
	f := &fakeRouter{status: map[string]string{}, build: "b11669-620d2225c", decode: 50, prefill: 700}
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
		if r.URL.Query().Get("autoload") != "false" {
			http.Error(w, "a model-routed request without autoload=false", http.StatusBadRequest)
			return
		}
		if m, _ := body["model"].(string); f.status[m] != "loaded" {
			http.Error(w, "model not loaded: "+m, http.StatusBadRequest)
			return
		}
	}
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch r.URL.Path {
	case "/models":
		f.record("models")
		var data []map[string]any
		for id, s := range f.status {
			data = append(data, map[string]any{"id": id, "status": map[string]any{"value": s}})
		}
		reply(map[string]any{"data": data})
	case "/props":
		f.record("props")
		reply(map[string]any{"build_info": f.build})
	case "/v1/messages":
		f.record("messages")
		reply(map[string]any{"content": []map[string]any{{"type": "tool_use", "name": "get_weather", "input": map[string]any{"city": "Paris"}}}})
	case "/tokenize":
		content, _ := body["content"].(string)
		toks := make([]int, len(strings.Fields(content)))
		reply(map[string]any{"tokens": toks})
	case "/detokenize":
		toks, _ := body["tokens"].([]any)
		reply(map[string]any{"content": strings.Repeat("w ", len(toks))})
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
			case "input_audio":
				audio = true
			}
		}
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
		kind, answer = "capacity", answer+"\n"+n
	}
	if audio {
		f.mu.Lock()
		f.audio = append(f.audio, body)
		f.mu.Unlock()
	}
	stream, _ := body["stream"].(bool)
	f.record(fmt.Sprintf("%s %v %s", kind, stream, body["model"]))
	timings := map[string]any{
		"prompt_n": len(strings.Fields(text.String())), "prompt_per_second": f.prefill,
		"predicted_n": 10, "predicted_per_second": f.decode,
	}
	if !stream {
		reply(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": answer}}},
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
	var data []map[string]any
	tokens := 0
	for i, in := range inputs {
		tokens += len(strings.Fields(in))
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
// repeated, out of a 98304MiB pool.
type fakeFootprint struct {
	mu   sync.Mutex
	free []int
	n    int
	err  error
}

func (f *fakeFootprint) Read(context.Context) (gate.Pool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return gate.Pool{}, f.err
	}
	free := f.free[min(f.n, len(f.free)-1)]
	f.n++
	return gate.Pool{UsedMiB: 98304 - free, TotalMiB: 98304}, nil
}
