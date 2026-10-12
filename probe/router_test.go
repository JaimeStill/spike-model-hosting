//go:build integration

package probe_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/JaimeStill/spike-model-hosting/profile"
)

// strixHalo is the profile the router serves; every expectation below is read from it.
const strixHalo = "../profiles/strix-halo.toml"

// The set-A models each capability probe sends to, by router ID. The profile types no role, so
// the probe names them; TestRouter fails if the profile no longer lists one. The embedding
// model is found by its kind instead.
const (
	chatModel   = "ggml-org/gpt-oss-120b-GGUF:MXFP4"
	visionModel = "ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0"
	audioModel  = "ggml-org/gemma-4-E4B-it-GGUF:Q8_0"
)

// embeddingDims is EmbeddingGemma 2's output width, per its model card.
const embeddingDims = 768

// samplingTolerance absorbs the float32 the server holds sampling in: top_p 0.95 reads back as
// 0.949999988079071.
const samplingTolerance = 1e-6

// requestTimeout bounds one request. The models are already loaded, so this covers generation
// only, which on gpt-oss-120b's reasoning takes tens of seconds.
const requestTimeout = 3 * time.Minute

// TestRouter probes the router SPIKE_ROUTER_URL names, serving the Strix Halo profile's render.
// Each check is a subtest, so a failure names the check. Every model-routed request carries
// autoload=false: the probe checks that the router loaded the models at startup, and never
// loads one itself.
func TestRouter(t *testing.T) {
	base := os.Getenv("SPIKE_ROUTER_URL")
	if base == "" {
		t.Skip("SPIKE_ROUTER_URL is unset: set it to the router to probe, such as http://<host>:<port>")
	}
	p, err := profile.Load(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	r := &router{base: strings.TrimSuffix(base, "/"), client: &http.Client{Timeout: requestTimeout}}

	models := make(map[string]profile.Model, len(p.Models))
	var embedding string
	for _, m := range p.Models {
		models[m.ID] = m
		if m.Kind == profile.KindEmbedding {
			embedding = m.ID
		}
	}
	for _, id := range []string{chatModel, visionModel, audioModel} {
		if _, ok := models[id]; !ok {
			t.Fatalf("%s doesn't list %s, which the probe sends to", strixHalo, id)
		}
	}
	if embedding == "" {
		t.Fatalf("%s lists no embedding model", strixHalo)
	}

	t.Run("health", func(t *testing.T) {
		var got struct {
			Status string `json:"status"`
		}
		r.get(t, "/health", &got)
		if got.Status != "ok" {
			t.Errorf("/health status = %q, want ok", got.Status)
		}
	})

	t.Run("models loaded", func(t *testing.T) {
		var got struct {
			Data []struct {
				ID     string `json:"id"`
				Status struct {
					Value string `json:"value"`
				} `json:"status"`
			} `json:"data"`
		}
		r.get(t, "/models", &got)
		status := make(map[string]string, len(got.Data))
		for _, m := range got.Data {
			status[m.ID] = m.Status.Value
		}
		for _, m := range p.Models {
			if s, ok := status[m.ID]; !ok {
				t.Errorf("/models doesn't list %s", m.ID)
			} else if s != "loaded" {
				t.Errorf("/models lists %s as %q, want loaded", m.ID, s)
			}
		}
	})

	t.Run("build", func(t *testing.T) {
		var got struct {
			BuildInfo string `json:"build_info"`
		}
		r.get(t, "/props", &got)
		t.Logf("router build_info: %s", got.BuildInfo)
		have, err := buildNumber(got.BuildInfo)
		if err != nil {
			t.Fatalf("/props build_info: %v", err)
		}
		floor, err := buildNumber(p.Engine.MinBuild)
		if err != nil {
			t.Fatalf("engine.min_build: %v", err)
		}
		if have < floor {
			t.Errorf("router build b%d is below the profile's min_build %s", have, p.Engine.MinBuild)
		}
	})

	t.Run("sampling", func(t *testing.T) {
		for _, m := range p.Models {
			if m.Sampling == nil {
				continue
			}
			t.Run(m.ID, func(t *testing.T) {
				var got struct {
					Settings struct {
						Params struct {
							Temperature float64 `json:"temperature"`
							TopP        float64 `json:"top_p"`
							TopK        int     `json:"top_k"`
							MinP        float64 `json:"min_p"`
						} `json:"params"`
					} `json:"default_generation_settings"`
				}
				r.get(t, "/props?model="+url.QueryEscape(m.ID), &got)
				params, s := got.Settings.Params, m.Sampling
				for _, f := range []struct {
					name      string
					got, want float64
				}{
					{"temperature", params.Temperature, *s.Temperature},
					{"top_p", params.TopP, *s.TopP},
					{"min_p", params.MinP, *s.MinP},
				} {
					if math.Abs(f.got-f.want) > samplingTolerance {
						t.Errorf("%s default %s = %v, want %v", m.ID, f.name, f.got, f.want)
					}
				}
				if params.TopK != *s.TopK {
					t.Errorf("%s default top_k = %d, want %d", m.ID, params.TopK, *s.TopK)
				}
			})
		}
	})

	t.Run("chat", func(t *testing.T) {
		text := r.chat(t, chatModel, "Reply with one word: ready.", 2048)
		t.Logf("%s: %q", chatModel, text)
		// The prompt asks for one word; casing and punctuation are the model's to choose.
		if !strings.Contains(strings.ToLower(text), "ready") {
			t.Errorf("%s: the reply %q doesn't say ready", chatModel, text)
		}
	})

	t.Run("tool call", func(t *testing.T) {
		body := map[string]any{
			"model":      chatModel,
			"max_tokens": 2048,
			"messages": []map[string]any{
				{"role": "user", "content": "What is the weather in Paris right now?"},
			},
			"tools": []map[string]any{{
				"name":        "get_weather",
				"description": "Get the current weather for a city.",
				"input_schema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"city": map[string]any{"type": "string"}},
					"required":   []string{"city"},
				},
			}},
			// The client constrains the shape it needs; the profile sets no tool policy.
			"tool_choice": map[string]any{"type": "any"},
		}
		var got struct {
			Content []struct {
				Type  string          `json:"type"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			} `json:"content"`
		}
		r.post(t, "/v1/messages", body, &got)
		for _, c := range got.Content {
			if c.Type == "tool_use" {
				t.Logf("%s: tool_use %s %s", chatModel, c.Name, c.Input)
				if c.Name != "get_weather" {
					t.Errorf("tool_use names %q, want get_weather", c.Name)
				}
				return
			}
		}
		t.Errorf("/v1/messages content has no tool_use block: %+v", got.Content)
	})

	t.Run("image", func(t *testing.T) {
		png := readFile(t, "testdata/shapes.png")
		text := r.chat(t, visionModel, []map[string]any{
			{"type": "text", "text": "Describe this image in one sentence."},
			{"type": "image_url", "image_url": map[string]any{
				"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
			}},
		}, 512)
		t.Logf("%s: %q", visionModel, text)
		// shapes.png is a red circle, a blue square, and a green triangle. Each shape and each
		// colour must be named, in any phrasing; which colour goes with which shape isn't checked.
		reply := strings.ToLower(text)
		for _, word := range []string{"circle", "square", "triangle", "red", "blue", "green"} {
			if !strings.Contains(reply, word) {
				t.Errorf("%s: the description %q doesn't name %s", visionModel, text, word)
			}
		}
	})

	t.Run("audio", func(t *testing.T) {
		wav := readFile(t, "testdata/phrase.wav")
		text := r.chat(t, audioModel, []map[string]any{
			{"type": "text", "text": "Transcribe this audio clip."},
			{"type": "input_audio", "input_audio": map[string]any{
				"data":   base64.StdEncoding.EncodeToString(wav),
				"format": "wav",
			}},
		}, 512)
		t.Logf("%s: %q", audioModel, text)
		// phrase.wav says "The access code is seven four two nine." The code may come back as
		// digits or words, spaced or not: compared with everything but letters and digits
		// stripped, either form passes.
		if got := alphanumeric(text); !strings.Contains(got, "7429") && !strings.Contains(got, "sevenfourtwonine") {
			t.Errorf("%s: the transcript %q doesn't carry the code 7429", audioModel, text)
		}
	})

	t.Run("embeddings", func(t *testing.T) {
		var got struct {
			Data []struct {
				Embedding []float64 `json:"embedding"`
			} `json:"data"`
		}
		r.post(t, "/v1/embeddings", map[string]any{
			"model":           embedding,
			"input":           "The access code is seven four two nine.",
			"encoding_format": "float",
		}, &got)
		if len(got.Data) != 1 {
			t.Fatalf("/v1/embeddings returned %d embeddings, want 1", len(got.Data))
		}
		if n := len(got.Data[0].Embedding); n != embeddingDims {
			t.Errorf("%s embedding has %d dimensions, want %d", embedding, n, embeddingDims)
		}
	})
}

// router is the router under probe.
type router struct {
	base   string
	client *http.Client
}

// get decodes the JSON reply to GET path into out, failing t on a transport error or a status
// other than 200.
func (r *router) get(t *testing.T, path string, out any) {
	t.Helper()
	r.do(t, http.MethodGet, withNoAutoload(path), nil, out)
}

// post sends body as JSON to path and decodes the reply into out, failing t as get does.
func (r *router) post(t *testing.T, path string, body, out any) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	r.do(t, http.MethodPost, withNoAutoload(path), b, out)
}

func (r *router) do(t *testing.T, method, path string, body []byte, out any) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, r.base+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	reply, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s %s: reading the reply: %v", method, path, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s %s: %s: %s", method, path, resp.Status, reply)
	}
	if err := json.Unmarshal(reply, out); err != nil {
		t.Fatalf("%s %s: decoding the reply: %v: %s", method, path, err, reply)
	}
}

// withNoAutoload adds autoload=false to path's query. The router reads it on every request it
// routes to a model, and refuses one whose model isn't loaded rather than load it; requests it
// answers itself ignore it.
func withNoAutoload(path string) string {
	if strings.Contains(path, "?") {
		return path + "&autoload=false"
	}
	return path + "?autoload=false"
}

// chat sends one user message with content, a string or a list of content parts, to model on
// the OpenAI chat completions endpoint, and returns the reply's text, failing t if it is empty.
func (r *router) chat(t *testing.T, model string, content any, maxTokens int) string {
	t.Helper()
	var got struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	r.post(t, "/v1/chat/completions", map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"messages":   []map[string]any{{"role": "user", "content": content}},
	}, &got)
	if len(got.Choices) == 0 {
		t.Fatalf("%s: the reply has no choices", model)
	}
	c := got.Choices[0]
	if strings.TrimSpace(c.Message.Content) == "" {
		t.Fatalf("%s: the reply's text is empty (finish_reason %q)", model, c.FinishReason)
	}
	return c.Message.Content
}

// buildNumber parses a llama.cpp build, such as b11452 or the build_info b11529-baef3ed9a, into
// its number.
func buildNumber(build string) (int, error) {
	digits, _, _ := strings.Cut(strings.TrimPrefix(build, "b"), "-")
	if !strings.HasPrefix(build, "b") || digits == "" {
		return 0, fmt.Errorf("%q is not a llama.cpp build such as b11452", build)
	}
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 0, fmt.Errorf("%q is not a llama.cpp build such as b11452", build)
	}
	return n, nil
}

// alphanumeric lowercases s and keeps only its letters and digits, so "7 4 2 9." and
// "Seven, four, two, nine" compare as 7429 and sevenfourtwonine.
func alphanumeric(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
