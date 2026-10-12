//go:build integration

package concurrent_test

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-harness-driver/harness"
	"github.com/JaimeStill/spike-harness-driver/model"
	"github.com/JaimeStill/spike-harness-driver/pi"

	"github.com/JaimeStill/spike-model-hosting/concurrent"
	"github.com/JaimeStill/spike-model-hosting/profile"
)

// The live run's chat model, the harness model, by router ID; the embedding model is the
// profile's embedding kind.
const liveChatModel = "ggml-org/gpt-oss-120b-GGUF:MXFP4"

// TestLiveRun runs the streams at once against the router SPIKE_ROUTER_URL names, keeping the
// profile's slots busy for two rounds. Pi joins when SPIKE_PI names its executable (such as
// `MISE_ENV=consumers mise which pi`); without it the model client's chat stream takes every
// slot. The models must be loaded already: the model client's requests carry autoload=false,
// and Pi's provider, which can't, runs only beside them.
func TestLiveRun(t *testing.T) {
	base := strings.TrimSuffix(os.Getenv("SPIKE_ROUTER_URL"), "/")
	if base == "" {
		t.Skip("SPIKE_ROUTER_URL is unset: set it to the router to run against")
	}
	p, err := profile.Load("../profiles/strix-halo.toml")
	if err != nil {
		t.Fatal(err)
	}
	var embedModel string
	for _, m := range p.Models {
		if m.Kind == profile.KindEmbedding {
			embedModel = m.ID
		}
	}
	client, err := model.New(model.Config{
		BaseURL: base + "/v1", Query: url.Values{"autoload": {"false"}}, HTTP: &http.Client{},
	})
	if err != nil {
		t.Fatal(err)
	}
	modelChat := concurrent.ChatStream{
		Name: "model-chat", Chatter: concurrent.ModelChat{Client: client, Model: liveChatModel, MaxTokens: 4096},
	}
	var chats []concurrent.ChatStream
	if command := os.Getenv("SPIKE_PI"); command != "" {
		state := t.TempDir()
		workers := concurrent.SplitSlots(p.Slots, 2)
		modelChat.Workers = workers[1]
		chats = append(chats, concurrent.ChatStream{Name: "pi", Workers: workers[0], Chatter: concurrent.HarnessChat{
			Driver: pi.Driver{
				Command: command, AgentDir: state + "/agent", SessionDir: state + "/sessions",
				CacheDir: state + "/cache", Env: []string{"LLAMA_BASE_URL=" + base},
			},
			Options: harness.Options{Provider: "llama.cpp", Model: liveChatModel, Dir: state, HarnessTools: []string{}},
		}})
	} else {
		modelChat.Workers = p.Slots
	}
	rep, err := concurrent.Run(t.Context(), concurrent.Config{
		Rounds: 2,
		Chat:   append(chats, modelChat),
		Embed: []concurrent.EmbedStream{{
			Name:     "model-embed",
			Embedder: concurrent.ModelEmbed{Client: client, Model: embedModel},
			Inputs: []string{
				"The access code is seven four two nine.",
				"A red circle, a blue square, and a green triangle.",
				"Rain is expected in the mountains by Thursday evening.",
				"Compile the program, then run its tests.",
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range rep.Chats {
		t.Logf("%s: %s in %s: %q", c.ID, c.Nonce, c.Duration, c.Answer)
	}
	for _, e := range rep.Embeds {
		t.Logf("%s: cosine %.6f", e.ID, e.Cosine)
	}
	for _, f := range rep.Failures() {
		t.Error(f)
	}
}
