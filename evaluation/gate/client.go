package gate

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Media are the image and audio clip the fitness and capacity stages send: the probe's, by
// default (LoadMedia).
type Media struct {
	PNG, WAV []byte
	// AudioSeconds is the WAV clip's length, the numerator of its RTFx.
	AudioSeconds float64
}

// LoadMedia reads the probe's image and audio clip from hosting/probe/testdata under the module
// root.
func LoadMedia(root string) (Media, error) {
	dir := filepath.Join(root, "hosting", "probe", "testdata")
	png, err := os.ReadFile(filepath.Join(dir, "shapes.png"))
	if err != nil {
		return Media{}, err
	}
	wav, err := os.ReadFile(filepath.Join(dir, "phrase.wav"))
	if err != nil {
		return Media{}, err
	}
	secs, err := WAVSeconds(wav)
	if err != nil {
		return Media{}, fmt.Errorf("phrase.wav: %w", err)
	}
	return Media{PNG: png, WAV: wav, AudioSeconds: secs}, nil
}

// WAVSeconds returns a RIFF WAV clip's length: its data chunk's size over its fmt chunk's byte
// rate.
func WAVSeconds(b []byte) (float64, error) {
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return 0, errors.New("not a RIFF WAVE file")
	}
	var byteRate, dataSize uint32
	for off := 12; off+8 <= len(b); {
		id, size := string(b[off:off+4]), binary.LittleEndian.Uint32(b[off+4:off+8])
		body := off + 8
		switch id {
		case "fmt ":
			if body+12 > len(b) {
				return 0, errors.New("a short fmt chunk")
			}
			byteRate = binary.LittleEndian.Uint32(b[body+8 : body+12])
		case "data":
			dataSize = size
		}
		off = body + int(size) + int(size%2)
	}
	if byteRate == 0 || dataSize == 0 {
		return 0, errors.New("no fmt byte rate or no data chunk")
	}
	return float64(dataSize) / float64(byteRate), nil
}

// client sends the stages' requests to the router. Every model-routed request carries
// autoload=false, so it never loads a model.
type client struct {
	base string
	hc   *http.Client
}

// timings is llama.cpp's own accounting of a completion.
type timings struct {
	CacheN          int     `json:"cache_n"`
	PromptN         int     `json:"prompt_n"`
	PromptPerSecond float64 `json:"prompt_per_second"`
	// PromptMS is the server's time evaluating the prompt, its media encoded with it: from the
	// slot taking the request to its first token, the sweep's time to first token.
	PromptMS           float64 `json:"prompt_ms"`
	PredictedN         int     `json:"predicted_n"`
	PredictedPerSecond float64 `json:"predicted_per_second"`
}

// completion is one chat completion's answer.
type completion struct {
	Text, Reasoning string
	// Finish is the choice's finish_reason: "stop", or "length" when the answer hit its
	// max_tokens.
	Finish  string
	Timings timings
	// Wall is the client's time from sending to the last byte; TTFT, to the first streamed token,
	// reasoning included, for a streamed one.
	Wall, TTFT time.Duration
}

// chat sends one user message with content, a string or content parts, to model, and returns
// the answer with its timings.
func (c *client) chat(ctx context.Context, model string, content any, maxTokens int) (completion, error) {
	return c.complete(ctx, chatBody(model, content, maxTokens, false))
}

// chatUncached is chat with the server's prompt cache off for the request (llama.cpp's
// cache_prompt false), so a timed request evaluates its whole prompt however often the same
// image or clip was sent before.
func (c *client) chatUncached(ctx context.Context, model string, content any, maxTokens int) (completion, error) {
	body := chatBody(model, content, maxTokens, false)
	body["cache_prompt"] = false
	return c.complete(ctx, body)
}

// complete sends a chat completion body and returns the answer with its timings.
func (c *client) complete(ctx context.Context, body map[string]any) (completion, error) {
	var got struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Timings *timings `json:"timings"`
	}
	start := time.Now()
	if err := c.post(ctx, "/v1/chat/completions", body, &got); err != nil {
		return completion{}, err
	}
	out := completion{Wall: time.Since(start)}
	if len(got.Choices) == 0 {
		return out, errors.New("the answer has no choices")
	}
	if got.Timings == nil {
		return out, errors.New("the answer carries no timings")
	}
	out.Text, out.Reasoning, out.Timings = got.Choices[0].Message.Content, got.Choices[0].Message.ReasoningContent, *got.Timings
	out.Finish = got.Choices[0].FinishReason
	return out, nil
}

func chatBody(model string, content any, maxTokens int, stream bool) map[string]any {
	return map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"stream":     stream,
		"messages":   []map[string]any{{"role": "user", "content": content}},
	}
}

// stream sends one streamed chat completion and measures its time to first token.
func (c *client) stream(ctx context.Context, model, prompt string, maxTokens int) (completion, error) {
	data, err := json.Marshal(chatBody(model, prompt, maxTokens, true))
	if err != nil {
		return completion{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/chat/completions?autoload=false", bytes.NewReader(data))
	if err != nil {
		return completion{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := c.hc.Do(req)
	if err != nil {
		return completion{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return completion{}, fmt.Errorf("POST /v1/chat/completions: %s: %s", resp.Status, bytes.TrimSpace(msg))
	}
	var out completion
	timed := false
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		payload, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok || payload == "[DONE]" {
			continue
		}
		var ch struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
				} `json:"delta"`
			} `json:"choices"`
			Timings *timings `json:"timings"`
		}
		if err := json.Unmarshal([]byte(payload), &ch); err != nil {
			return completion{}, fmt.Errorf("a stream chunk: %w", err)
		}
		for _, choice := range ch.Choices {
			if out.TTFT == 0 && (choice.Delta.Content != "" || choice.Delta.ReasoningContent != "") {
				out.TTFT = time.Since(start)
			}
			out.Text += choice.Delta.Content
		}
		if ch.Timings != nil {
			out.Timings, timed = *ch.Timings, true
		}
	}
	if err := sc.Err(); err != nil {
		return completion{}, err
	}
	out.Wall = time.Since(start)
	switch {
	case !timed:
		return out, errors.New("the stream carried no timings")
	case out.TTFT == 0:
		return out, errors.New("the stream carried no token")
	}
	return out, nil
}

// toolCall asks model, on the Anthropic messages endpoint, for the weather in Paris with one
// tool offered and a tool call required, as the probe does, and returns the tool it called, or
// "" when it called none.
func (c *client) toolCall(ctx context.Context, model string) (string, error) {
	body := map[string]any{
		"model":      model,
		"max_tokens": 2048,
		"messages":   []map[string]any{{"role": "user", "content": "What is the weather in Paris right now?"}},
		"tools": []map[string]any{{
			"name":        "get_weather",
			"description": "Get the current weather for a city.",
			"input_schema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"city": map[string]any{"type": "string"}},
				"required":   []string{"city"},
			},
		}},
		"tool_choice": map[string]any{"type": "any"},
	}
	var got struct {
		Content []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"content"`
	}
	if err := c.post(ctx, "/v1/messages", body, &got); err != nil {
		return "", err
	}
	for _, b := range got.Content {
		if b.Type == "tool_use" {
			return b.Name, nil
		}
	}
	return "", nil
}

// embedding is one embeddings request's answer.
type embedding struct {
	Vectors      [][]float32
	PromptTokens int
	Wall         time.Duration
}

// embed embeds inputs in one request to model.
func (c *client) embed(ctx context.Context, model string, inputs []string) (embedding, error) {
	var got struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	start := time.Now()
	if err := c.post(ctx, "/v1/embeddings", map[string]any{"model": model, "input": inputs, "encoding_format": "float"}, &got); err != nil {
		return embedding{}, err
	}
	out := embedding{Wall: time.Since(start), PromptTokens: got.Usage.PromptTokens, Vectors: make([][]float32, len(inputs))}
	for _, d := range got.Data {
		if d.Index < 0 || d.Index >= len(inputs) {
			return out, fmt.Errorf("the answer's index %d is outside the %d inputs", d.Index, len(inputs))
		}
		out.Vectors[d.Index] = d.Embedding
	}
	return out, nil
}

// metrics reads model's llama.cpp counters from the router's /metrics, by name without the
// llamacpp: prefix.
func (c *client) metrics(ctx context.Context, model string) (map[string]float64, error) {
	u := c.base + "/metrics?" + url.Values{"model": {model}, "autoload": {"false"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /metrics: %s", resp.Status)
	}
	out := map[string]float64{}
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		name, value, ok := strings.Cut(sc.Text(), " ")
		name, counter := strings.CutPrefix(name, "llamacpp:")
		if !ok || !counter {
			continue
		}
		if v, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			out[name] = v
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("GET /metrics for %s: no llamacpp counter", model)
	}
	return out, nil
}

// fitText returns text exactly n of model's tokens long, its first words a nonce unique to the
// run so no cache holds any of it, cut from filler with the router's /tokenize and /detokenize.
func (c *client) fitText(ctx context.Context, model string, n int) (string, error) {
	nonce, err := newNonce()
	if err != nil {
		return "", err
	}
	var text strings.Builder
	fmt.Fprintf(&text, "Run %s. Read the passage below, then answer the question after it.\n\n", nonce)
	// Each sentence is about twenty tokens, so n/10 sentences leave the cut room to spare.
	for i := range n/10 + 1 {
		fmt.Fprintf(&text, "%s %s %s %s, and wrote it down in ledger %d. ",
			subjects[i%len(subjects)], verbs[(i/3)%len(verbs)], objects[(i/7)%len(objects)], times[(i/11)%len(times)], i+1)
	}
	var tok struct {
		Tokens []int `json:"tokens"`
	}
	if err := c.post(ctx, "/tokenize", map[string]any{"model": model, "content": text.String()}, &tok); err != nil {
		return "", err
	}
	if len(tok.Tokens) < n {
		return "", fmt.Errorf("the filler is %d tokens, short of the %d wanted", len(tok.Tokens), n)
	}
	var detok struct {
		Content string `json:"content"`
	}
	if err := c.post(ctx, "/detokenize", map[string]any{"model": model, "tokens": tok.Tokens[:n]}, &detok); err != nil {
		return "", err
	}
	return detok.Content, nil
}

// The words fitText's filler is made of.
var (
	subjects = []string{"The harbor master", "A surveyor", "The night clerk", "An old cartographer", "The ferry pilot", "A visiting botanist", "The archivist", "A railway engineer"}
	verbs    = []string{"recorded", "measured", "described", "sketched", "questioned", "catalogued", "mapped", "revisited"}
	objects  = []string{"the tide tables", "the northern ridge", "a flooded quarry", "the lighthouse stairs", "the river delta", "a ruined mill", "the salt flats", "the signal tower"}
	times    = []string{"before dawn", "in late autumn", "during the storm", "after the census", "at the spring fair", "over three winters", "on the last ferry", "beneath a red moon"}
)

// newNonce returns a nonce unique to the run.
func newNonce() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// imagePart and audioPart are the media's content parts.
func imagePart(png []byte) map[string]any {
	return map[string]any{"type": "image_url", "image_url": map[string]any{
		"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
	}}
}

func audioPart(wav []byte) map[string]any {
	return map[string]any{"type": "input_audio", "input_audio": map[string]any{
		"data": base64.StdEncoding.EncodeToString(wav), "format": "wav",
	}}
}

func textPart(text string) map[string]any { return map[string]any{"type": "text", "text": text} }

// post sends in as JSON to path, with autoload=false, and decodes the answer into out.
func (c *client) post(ctx context.Context, path string, in, out any) error {
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path+"?autoload=false", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("POST %s: %s: %s", path, resp.Status, bytes.TrimSpace(msg))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	return nil
}
