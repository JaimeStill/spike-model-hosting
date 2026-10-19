package main_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// runPrefix runs evaluate's prefix step from the repository root with args and env, as run does.
func runPrefix(t *testing.T, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	return run(t, env, append([]string{"prefix"}, args...)...)
}

// Set A's generative models, which the fake router lists as loaded beside the embedding model.
var generative = []string{
	"ggml-org/gpt-oss-120b-GGUF:MXFP4",
	"ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0",
	"ggml-org/gemma-4-E4B-it-GGUF:Q8_0",
}

const embedding = "ggml-org/embeddinggemma-2-GGUF:Q8_0"

// TestPrefixUsage shows the step refuses a call it can't run with exit status 2 and its usage, and
// prints its usage on -h.
func TestPrefixUsage(t *testing.T) {
	cases := map[string]struct {
		env  []string
		args []string
		want string
	}{
		"no router":     {nil, nil, "no router"},
		"one request":   {[]string{"SPIKE_ROUTER_URL=http://127.0.0.1:1"}, []string{"-requests", "1"}, "-requests"},
		"empty prefix":  {nil, []string{"-router", "http://127.0.0.1:1", "-prefix-tokens", "0"}, "-prefix-tokens"},
		"no tokens out": {nil, []string{"-router", "http://127.0.0.1:1", "-max-tokens", "0"}, "-max-tokens"},
		"bad slot":      {nil, []string{"-router", "http://127.0.0.1:1", "-slot", "-2"}, "-slot"},
		"unknown flag":  {nil, []string{"-router", "http://127.0.0.1:1", "-nope"}, "-nope"},
		"argument":      {nil, []string{"-router", "http://127.0.0.1:1", "extra"}, "unexpected arguments"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, errOut, code := runPrefix(t, tc.env, tc.args...)
			if code != 2 || !strings.Contains(errOut, tc.want) || !strings.Contains(errOut, "usage:") {
				t.Errorf("exit %d, stderr %q; want 2, naming %q, with the usage", code, errOut, tc.want)
			}
		})
	}
	out, _, code := runPrefix(t, nil, "-h")
	if code != 0 || !strings.Contains(out, "usage:") {
		t.Errorf("-h: exit %d, stdout %q; want 0 and the usage", code, out)
	}
}

// fakeRouter is a router whose tokenizer splits on spaces, one token a word, and whose every
// model keeps one cache per slot: a request is served from the cache for as many leading words
// as it shares with the slot's last prompt.
type fakeRouter struct {
	mu sync.Mutex
	// unloaded is a model /models lists as unloaded.
	unloaded string
	// cache is each model's slots' last prompts, in words.
	cache map[string]map[int][]string
	// slots are the id_slot values requests carried, -1 for none.
	slots []int
	// noAutoload counts the model requests that lacked autoload=false.
	noAutoload int
	// prompts are the user messages, in the order they came.
	prompts []string
}

func (f *fakeRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path != "/models" && r.URL.Path != "/props" && r.URL.Query().Get("autoload") != "false" {
		f.noAutoload++
	}
	var in struct {
		Model    string `json:"model"`
		Content  string `json:"content"`
		Tokens   []int  `json:"tokens"`
		IDSlot   *int   `json:"id_slot"`
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	switch r.URL.Path {
	case "/models":
		var data []map[string]any
		for _, id := range append(generative, embedding) {
			status := "loaded"
			if id == f.unloaded {
				status = "unloaded"
			}
			data = append(data, map[string]any{"id": id, "status": map[string]string{"value": status}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	case "/props":
		_, _ = fmt.Fprint(w, `{"build_info":"b1-test"}`)
	case "/tokenize":
		// A word's token is its position in the text, so detokenize can give it back.
		words := strings.Fields(in.Content)
		tokens := make([]int, len(words))
		for i := range words {
			tokens[i] = i
		}
		f.prompts = append(f.prompts, in.Content)
		_ = json.NewEncoder(w).Encode(map[string]any{"tokens": tokens})
	case "/detokenize":
		words := strings.Fields(f.prompts[len(f.prompts)-1])
		f.prompts = f.prompts[:len(f.prompts)-1]
		_ = json.NewEncoder(w).Encode(map[string]string{"content": strings.Join(words[:len(in.Tokens)], " ")})
	case "/v1/chat/completions":
		slot := -1
		if in.IDSlot != nil {
			slot = *in.IDSlot
		}
		f.slots = append(f.slots, slot)
		words := strings.Fields(in.Messages[0].Content)
		if f.cache == nil {
			f.cache = map[string]map[int][]string{}
		}
		if f.cache[in.Model] == nil {
			f.cache[in.Model] = map[int][]string{}
		}
		// Unpinned, a request goes to slot 0, as the real server's similarity choice would.
		prev := f.cache[in.Model][max(slot, 0)]
		n := 0
		for n < len(prev) && n < len(words) && prev[n] == words[n] {
			n++
		}
		f.cache[in.Model][max(slot, 0)] = words
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":null}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"Thinking\"}}]}\n\n")
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{}}],\"timings\":{\"cache_n\":%d,\"prompt_n\":%d,\"prompt_ms\":%d}}\n\n", n, len(words)-n, len(words)-n)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	default:
		http.NotFound(w, r)
	}
}

// rowRE matches one row of the step's table: model, request, cache_n, prompt_n.
var rowRE = regexp.MustCompile(`(?m)^\s*(\S+)\s+(\d+)\s+(\d+)\s+(\d+)\s+\d+\s+\d+\s+\d+\s*$`)

// TestPrefixMeasure shows the step sends each generative model its requests, never letting one load
// a model, prints the server's cache accounting for each, cold first and warm after, and pins every
// request to the slot -slot names.
func TestPrefixMeasure(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		slot int
	}{
		"unpinned": {nil, -1},
		"pinned":   {[]string{"-slot", "2"}, 2},
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeRouter{}
			srv := httptest.NewServer(f)
			defer srv.Close()
			args := append([]string{"-router", srv.URL, "-prefix-tokens", "300", "-requests", "3"}, tc.args...)
			out, errOut, code := runPrefix(t, nil, args...)
			if code != 0 {
				t.Fatalf("exit %d, stderr %q", code, errOut)
			}
			if !strings.Contains(out, "build b1-test, set a\n") {
				t.Errorf("the output doesn't name the build and the set:\n%s", out)
			}
			rows := rowRE.FindAllStringSubmatch(out, -1)
			if len(rows) != 3*len(generative) {
				t.Fatalf("%d rows, want %d:\n%s", len(rows), 3*len(generative), out)
			}
			for i, row := range rows {
				model, req := row[1], row[2]
				cacheN, _ := strconv.Atoi(row[3])
				if model != generative[i/3] || req != strconv.Itoa(i%3+1) {
					t.Errorf("row %d is %s request %s, want %s request %d", i, model, req, generative[i/3], i%3+1)
				}
				// The prefix is 300 words; a question shares none of its words with the next, so
				// a warm request's cache covers the prefix and no more.
				want := 300
				if req == "1" {
					want = 0
				}
				if cacheN != want {
					t.Errorf("%s request %s: cache_n %d, want %d", model, req, cacheN, want)
				}
			}
			for _, s := range f.slots {
				if s != tc.slot {
					t.Errorf("a request carried id_slot %d, want %d (-1 is none)", s, tc.slot)
				}
			}
			if f.noAutoload != 0 {
				t.Errorf("%d model requests lacked autoload=false", f.noAutoload)
			}
		})
	}
}

// TestPrefixRefusesUnloaded shows the step sends nothing when a model it would send to isn't
// loaded.
func TestPrefixRefusesUnloaded(t *testing.T) {
	f := &fakeRouter{unloaded: generative[1]}
	srv := httptest.NewServer(f)
	defer srv.Close()
	_, errOut, code := runPrefix(t, nil, "-router", srv.URL)
	if code != 1 || !strings.Contains(errOut, generative[1]) || !strings.Contains(errOut, "never loads") {
		t.Errorf("exit %d, stderr %q; want 1, naming %s", code, errOut, generative[1])
	}
	if len(f.slots) != 0 {
		t.Errorf("%d chat requests sent, want none", len(f.slots))
	}
}

// TestPrefixRefusesUnknownSet shows the step refuses a set the profile doesn't name, before it
// sends the router anything.
func TestPrefixRefusesUnknownSet(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected", http.StatusTeapot)
	}))
	defer srv.Close()
	_, errOut, code := runPrefix(t, []string{"SPIKE_SET=z"}, "-router", srv.URL)
	if code != 2 || !strings.Contains(errOut, `no set "z"`) {
		t.Errorf("exit %d, stderr %q; want 2, naming the unknown set", code, errOut)
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("the router got %d requests, want 0", n)
	}
}
