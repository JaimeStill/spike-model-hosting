package profile_test

import (
	"bytes"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
)

// update rewrites the golden files from the current render: go test ./hosting/profile -update.
var update = flag.Bool("update", false, "rewrite golden files")

const (
	skeleton  = "testdata/skeleton.toml"
	strixHalo = "../../devices/strix-halo/profile.toml"
	// pinned is personal-agents' 96GiB preset, copied at the commit testdata/README.md names.
	pinned = "testdata/unified-96gb.ini"
)

func TestLoadAcceptsComplete(t *testing.T) {
	p, err := profile.Load(skeleton)
	if err != nil {
		t.Fatalf("Load(%s): %v", skeleton, err)
	}
	if p.HostClass != profile.HostStrixHalo || len(p.Models) != 2 {
		t.Errorf("Load(%s) = host class %q, %d models; want %q, 2", skeleton, p.HostClass, len(p.Models), profile.HostStrixHalo)
	}
}

// TestLoadNamesMissingField removes one required key at a time from the complete profile, and
// expects the error to name it by its TOML key path. A model's key is removed from the first
// model, models[0].
func TestLoadNamesMissingField(t *testing.T) {
	base, err := os.ReadFile(skeleton)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		drop  string // the line removed, matched by its prefix
		field string // the field the error must name
	}{
		{"host_class =", "host_class"},
		{"bind =", "bind"},
		{"kv =", "kv"},
		{"name = \"llama.cpp\"", "engine.name"},
		{"backend =", "engine.backend"},
		{"min_build =", "engine.min_build"},
		{"gpu_layers =", "defaults.gpu_layers"},
		{"checkpoint_min_step =", "defaults.checkpoint_min_step"},
		{"pool_mib =", "sets[0].memory_budget.pool_mib"},
		{"measured_mib =", "sets[0].memory_budget.measured_mib"},
		{"build =", "sets[0].memory_budget.build"},
		{"name = \"a\"", "sets[0].name"},
		{"chat = ", "sets[0].roles.chat"},
		{"embedding = ", "sets[0].roles.embedding"},
		{"id =", "models[0].id"},
		{"publisher =", "models[0].publisher"},
		{"origin =", "models[0].origin"},
		{"licence =", "models[0].licence"},
		{"card_url =", "models[0].card_url"},
		{"card_context =", "models[0].card_context"},
		{"c = 131072", "models[0].c"},
		{"c = 32768", "defaults.c"},
		{"slots =", "models[0].slots"},
		{"slots_reason =", "models[0].slots_reason"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			path := writeProfile(t, dropLine(t, string(base), tt.drop))
			assertFails(t, path, tt.field+": required")
		})
	}

	t.Run("models[0].c_reason", func(t *testing.T) {
		text := string(base)
		before, _, _ := strings.Cut(text, "c_reason =")
		path := writeProfile(t, before)
		assertFails(t, path, "models[0].c_reason: required")
	})
	t.Run("engine", func(t *testing.T) {
		path := writeProfile(t, dropTable(t, string(base), "[engine]"))
		assertFails(t, path, "engine.name: required", "engine.backend: required", "engine.min_build: required")
	})
	t.Run("sets[0].memory_budget", func(t *testing.T) {
		path := writeProfile(t, dropTable(t, string(base), "[sets.memory_budget]"))
		assertFails(t, path, "sets[0].memory_budget.pool_mib: required", "sets[0].memory_budget.build: required")
	})
	t.Run("sets", func(t *testing.T) {
		text := dropTable(t, dropTable(t, string(base), "[sets.memory_budget]"), "[sets.roles]")
		path := writeProfile(t, dropTable(t, text, "[[sets]]"))
		assertFails(t, path, "sets: required", `models[0].id: "ggml-org/gpt-oss-120b-GGUF:MXFP4" is in no set`)
	})
	t.Run("models", func(t *testing.T) {
		text := string(base)
		path := writeProfile(t, text[:strings.Index(text, "[[models]]")])
		assertFails(t, path, "models: required")
	})
}

func TestLoadNamesInvalidValue(t *testing.T) {
	base, err := os.ReadFile(skeleton)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		old, new string
		field    string
	}{
		{`host_class = "strix-halo"`, `host_class = "laptop"`, "host_class"},
		{`bind = "tailnet"`, `bind = "lan"`, "bind"},
		{`kv = "unified"`, `kv = "shared"`, "kv"},
		{`min_build = "b11452"`, `min_build = "0.4.0"`, "engine.min_build"},
		{`slots = 4`, `slots = 0`, "models[0].slots"},
		{`slots = 4`, `slots = -1`, "models[0].slots"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			path := writeProfile(t, strings.Replace(string(base), tt.old, tt.new, 1))
			assertFails(t, path, tt.field+":")
		})
	}
}

// TestLoadAcceptsCUDAHostClass shows the schema allows the CUDA host class.
func TestLoadAcceptsCUDAHostClass(t *testing.T) {
	base, err := os.ReadFile(skeleton)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(base), `host_class = "strix-halo"`, `host_class = "nvidia-cuda"`, 1)
	text = strings.Replace(text, `backend = "vulkan"`, `backend = "cuda"`, 1)
	if _, err := profile.Load(writeProfile(t, text)); err != nil {
		t.Errorf("Load(CUDA profile): %v", err)
	}
}

// TestRenderPresets compares each profile's rendered presets with its golden file.
func TestRenderPresets(t *testing.T) {
	for _, tt := range []struct{ profile, golden string }{
		{skeleton, "testdata/skeleton.ini"},
		{strixHalo, "testdata/strix-halo.ini"},
	} {
		t.Run(tt.golden, func(t *testing.T) {
			got := render(t, tt.profile)
			if *update {
				if err := os.WriteFile(tt.golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(tt.golden)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("RenderPresets differs from %s (rerun with -update to accept):\n--- got\n%s\n--- want\n%s", tt.golden, got, want)
			}
		})
	}
}

// TestStrixHaloMatchesPinnedPreset renders the Strix Halo profile and compares the values each of
// set A's models, its startup set, runs at with the pinned copy of personal-agents' 96GiB preset.
// Each side is compared as the router applies it, [*] and then the model's section over it, so a
// key the pinned copy sets in [*] and the profile in each section (parallel, each model's own
// slots since screen's slice 2) compares as the same value. The differences allowed are
// load-on-startup = true and hf-repo = the section's name on each of set A's sections, and
// mtmd-batch-max-tokens on a model that caps its image batch (the vision model's, from
// evaluation's slice 8), which the profile renders and the pinned copy lacks. Every other set's
// model renders without load-on-startup and is left out of the comparison: the pinned copy
// predates the candidates.
func TestStrixHaloMatchesPinnedPreset(t *testing.T) {
	p, err := profile.Load(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	if s := p.StartupSet(); s == nil || s.Name != "a" {
		t.Fatalf("StartupSet() = %+v; want set a", s)
	}
	startup := p.StartupSet().Models
	got := parseINI(t, render(t, strixHalo))
	text, err := os.ReadFile(pinned)
	if err != nil {
		t.Fatal(err)
	}
	want := applied(parseINI(t, string(text)))

	models := 0
	for name, keys := range got {
		switch {
		case name == "*":
			continue
		case !slices.Contains(startup, name):
			if v, ok := keys["load-on-startup"]; ok {
				t.Errorf("[%s] load-on-startup = %q; want the key absent outside set A", name, v)
			}
			delete(got, name)
			continue
		}
		models++
		keys = applied(map[string]map[string]string{"*": got["*"], name: keys})[name]
		got[name] = keys
		if keys["load-on-startup"] != "true" {
			t.Errorf("[%s] load-on-startup = %q; want true", name, keys["load-on-startup"])
		}
		if keys["hf-repo"] != name {
			t.Errorf("[%s] hf-repo = %q; want the section's name", name, keys["hf-repo"])
		}
		delete(keys, "load-on-startup")
		delete(keys, "hf-repo")
		if m := p.Model(name); m != nil && m.MtmdBatchMaxTokens != 0 {
			if want := strconv.Itoa(m.MtmdBatchMaxTokens); keys["mtmd-batch-max-tokens"] != want {
				t.Errorf("[%s] mtmd-batch-max-tokens = %q; want %s", name, keys["mtmd-batch-max-tokens"], want)
			}
			delete(keys, "mtmd-batch-max-tokens")
		}
	}
	if models != 4 {
		t.Errorf("rendered %d of set A's model sections; want 4", models)
	}
	delete(got, "*")
	for name, wantKeys := range want {
		gotKeys, ok := got[name]
		if !ok {
			t.Errorf("[%s]: in %s, not rendered", name, pinned)
			continue
		}
		for key, w := range wantKeys {
			g, ok := gotKeys[key]
			switch {
			case !ok:
				t.Errorf("[%s] %s: in %s, not rendered", name, key, pinned)
			case !sameValue(g, w):
				t.Errorf("[%s] %s = %q; %s has %q", name, key, g, pinned, w)
			}
		}
		for key, g := range gotKeys {
			if _, ok := wantKeys[key]; !ok {
				t.Errorf("[%s] %s = %q: rendered, not in %s", name, key, g, pinned)
			}
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("[%s]: rendered, not in %s", name, pinned)
		}
	}
}

// TestLoadRefusesSetsAndRoles changes the Strix Halo profile so it breaks one rule of its sets,
// roles or licences, and expects the error that names it.
func TestLoadRefusesSetsAndRoles(t *testing.T) {
	base, err := os.ReadFile(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	const (
		gptOSS    = "ggml-org/gpt-oss-120b-GGUF:MXFP4"
		embedding = "ggml-org/embeddinggemma-2-GGUF:Q8_0"
	)
	text := string(base)
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			"no startup set",
			replace(t, text, "startup = true", "startup = false"),
			"sets: required, one set with startup = true",
		},
		{
			"two startup sets",
			text + extraSet("b", true, gptOSS),
			`sets: ["a" "b"] are all startup sets; want exactly one`,
		},
		{
			"a set over four models",
			replace(t, extraModel(t, text, "example/fifth-GGUF:Q4_0"), "startup = true\nmodels = [\n", "startup = true\nmodels = [\n  \"example/fifth-GGUF:Q4_0\",\n"),
			"sets[0].models: 5 models, over the router's 4",
		},
		{
			"a set naming a model the profile doesn't list",
			text + extraSet("b", false, gptOSS, "example/unlisted-GGUF:Q4_0"),
			`sets[3].models: "example/unlisted-GGUF:Q4_0" is not a model the profile lists`,
		},
		{
			"a set with no chat model",
			text + extraSet("b", false, embedding),
			"sets[3].models: required, at least one chat model",
		},
		{
			"an unknown role",
			replace(t, text, `roles = ["chat"]`, `roles = ["chat", "reasoning"]`),
			`models[0].roles: "reasoning" is not one of`,
		},
		{
			"a missing licence",
			replace(t, text, "licence = \"Apache-2.0\"\nroles = [\"chat\"]", `roles = ["chat"]`),
			"models[0].licence: required",
		},
		{
			"a model in no set",
			extraModel(t, text, "example/fifth-GGUF:Q4_0"),
			`models[6].id: "example/fifth-GGUF:Q4_0" is in no set`,
		},
		{
			"a set named twice",
			text + extraSet("a", false, gptOSS),
			`sets[3].name: "a" is listed twice`,
		},
		{
			"a set listing a model twice",
			text + extraSet("b", false, gptOSS, gptOSS),
			`sets[3].models: "ggml-org/gpt-oss-120b-GGUF:MXFP4" is listed twice`,
		},
		{
			"no role",
			replace(t, text, `roles = ["chat"]`, `roles = []`),
			"models[0].roles: required",
		},
		{
			"a role listed twice",
			replace(t, text, `roles = ["chat"]`, `roles = ["chat", "chat"]`),
			"models[0].roles: [\"chat\" \"chat\"] lists a role twice",
		},
		{
			"a generative model taking the embedding role",
			replace(t, text, `roles = ["chat"]`, `roles = ["chat", "embedding"]`),
			"models[0].roles: a generative model can't take the embedding role",
		},
		{
			"a set assigning no chat model",
			replace(t, text, "chat = \"ggml-org/gpt-oss-120b-GGUF:MXFP4\"\n", ""),
			"sets[0].roles.chat: required",
		},
		{
			"a set assigning no embedding model",
			replace(t, text, "audio = \"ggml-org/gemma-4-E4B-it-GGUF:Q8_0\"\nembedding = \"ggml-org/embeddinggemma-2-GGUF:Q8_0\"\n\n# The binding test", "audio = \"ggml-org/gemma-4-E4B-it-GGUF:Q8_0\"\n\n# The binding test"),
			"sets[0].roles.embedding: required",
		},
		{
			"a role assigned to a model outside the set",
			replace(t, text, "vision = \"ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0\"", "vision = \"example/unlisted-GGUF:Q4_0\""),
			`sets[0].roles.vision: "example/unlisted-GGUF:Q4_0" is not one of the set's models`,
		},
		{
			"a role assigned to a model that doesn't declare it",
			replace(t, text, "Q4_0\"\naudio = \"ggml-org/gemma-4-E4B-it-GGUF:Q8_0\"", "Q4_0\"\naudio = \"ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0\""),
			`sets[0].roles.audio: "ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0" doesn't declare the audio role`,
		},
		{
			"the chat role assigned to the embedding model",
			replace(t, text, "chat = \"ggml-org/gpt-oss-120b-GGUF:MXFP4\"", "chat = \"ggml-org/embeddinggemma-2-GGUF:Q8_0\""),
			`sets[0].roles.chat: "ggml-org/embeddinggemma-2-GGUF:Q8_0" doesn't declare the chat role`,
		},
		{
			"an embedding model without its width",
			replace(t, text, "dims = 768\n", ""),
			"models[3].dims: required, a positive integer",
		},
		{
			"a generative model with a width",
			replace(t, text, "bits_per_weight = 4.34\nkind = \"generative\"\n", "bits_per_weight = 4.34\nkind = \"generative\"\ndims = 768\n"),
			"models[0].dims: only an embedding model has an output width",
		},
		{
			"a context over the card's",
			replace(t, text, "card_context = 131072\nactive_params_b = 5.1", "card_context = 65536\nactive_params_b = 5.1"),
			"models[0].c: 131072 is over the card's context, 65536",
		},
		{
			"active parameters without bits per weight",
			replace(t, text, "bits_per_weight = 4.34\n", ""),
			"models[0].active_params_b: set with bits_per_weight, or neither",
		},
		{
			"bits per weight over 32",
			replace(t, text, "bits_per_weight = 4.34\n", "bits_per_weight = 40.0\n"),
			"models[0].bits_per_weight: 40 is over 32",
		},
		{
			"a negative bandwidth",
			replace(t, text, "memory_bandwidth_gb_s = 256\n", "memory_bandwidth_gb_s = -1\n"),
			"memory_bandwidth_gb_s: -1 is below 0",
		},
		{
			"an embedding model taking another role",
			replace(t, text, `roles = ["embedding"]`, `roles = ["embedding", "chat"]`),
			"models[3].roles: an embedding model takes the embedding role alone",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertFails(t, writeProfile(t, tt.text), tt.want)
		})
	}
}

// TestLoadAcceptsOptionalRoles shows a set may leave vision and audio unassigned, and may
// assign one model several roles, as a candidate set whose main model takes images does.
func TestLoadAcceptsOptionalRoles(t *testing.T) {
	base, err := os.ReadFile(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	text := string(base)
	text = replace(t, text, "vision = \"ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0\"\naudio = \"ggml-org/gemma-4-E4B-it-GGUF:Q8_0\"\n", "")
	p, err := profile.Load(writeProfile(t, text))
	if err != nil {
		t.Fatalf("Load(no vision or audio model): %v", err)
	}
	if got, want := p.Sets[0].Roles.Models(), []string{"ggml-org/gpt-oss-120b-GGUF:MXFP4", "ggml-org/embeddinggemma-2-GGUF:Q8_0"}; !slices.Equal(got, want) {
		t.Errorf("Roles.Models() = %q; want %q", got, want)
	}

	const gemma = "ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0"
	text = replace(t, string(base), "chat = \"ggml-org/gpt-oss-120b-GGUF:MXFP4\"", fmt.Sprintf("chat = %q", gemma))
	p, err = profile.Load(writeProfile(t, text))
	if err != nil {
		t.Fatalf("Load(chat and vision on one model): %v", err)
	}
	want := []string{gemma, "ggml-org/gemma-4-E4B-it-GGUF:Q8_0", "ggml-org/embeddinggemma-2-GGUF:Q8_0"}
	if got := p.Sets[0].Roles.Models(); !slices.Equal(got, want) {
		t.Errorf("Roles.Models() = %q; want %q, each model once", got, want)
	}
}

// TestSet shows the lookup the instruments share: no name is the startup set, a name is that
// set, and an unknown name fails naming the profile's sets.
func TestSet(t *testing.T) {
	base, err := os.ReadFile(skeleton)
	if err != nil {
		t.Fatal(err)
	}
	const (
		other     = "example/other-GGUF:Q4_0"
		embedding = "ggml-org/embeddinggemma-2-GGUF:Q8_0"
	)
	p, err := profile.Load(writeProfile(t, extraModel(t, string(base), other)+extraSet("b", false, other, embedding)))
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"": "a", "a": "a", "b": "b"} {
		s, err := p.Set(name)
		if err != nil || s.Name != want {
			t.Errorf("Set(%q) = %+v, %v; want set %s", name, s, err, want)
		}
	}
	if s, _ := p.Set("b"); s.Roles.Chat != other || s.Roles.Embedding != embedding {
		t.Errorf("Set(b).Roles = %+v; want chat %s, embedding %s", s.Roles, other, embedding)
	}
	_, err = p.Set("z")
	if err == nil || !strings.Contains(err.Error(), `no set "z"`) || !strings.Contains(err.Error(), `["a" "b"]`) {
		t.Errorf("Set(z) = %v; want an error naming z and the sets a and b", err)
	}
}

// TestRenderPresetsStartupSetOnly adds a second set, not the startup set, and shows its model
// renders without load-on-startup while the startup set's model keeps it.
func TestRenderPresetsStartupSetOnly(t *testing.T) {
	base, err := os.ReadFile(skeleton)
	if err != nil {
		t.Fatal(err)
	}
	const other = "example/other-GGUF:Q4_0"
	text := extraModel(t, string(base), other) + extraSet("b", false, other, "ggml-org/embeddinggemma-2-GGUF:Q8_0")
	p, err := profile.Load(writeProfile(t, text))
	if err != nil {
		t.Fatal(err)
	}
	if s := p.StartupSet(); s == nil || s.Name != "a" {
		t.Errorf("StartupSet() = %+v; want set a", s)
	}
	var b bytes.Buffer
	if err := p.RenderPresets(&b); err != nil {
		t.Fatal(err)
	}
	got := parseINI(t, b.String())
	if v := got["ggml-org/gpt-oss-120b-GGUF:MXFP4"]["load-on-startup"]; v != "true" {
		t.Errorf("startup set's model: load-on-startup = %q; want true", v)
	}
	if v, ok := got[other]["load-on-startup"]; ok {
		t.Errorf("other set's model: load-on-startup = %q; want the key absent", v)
	}
	if _, ok := got[other]; !ok {
		t.Errorf("other set's model [%s] not rendered:\n%s", other, b.String())
	}
}

// TestRenderPresetsCommentsReasons shows each model's reasons render as a comment above its
// section.
func TestRenderPresetsCommentsReasons(t *testing.T) {
	got := render(t, strixHalo)
	for _, tt := range []struct{ phrase, section string }{
		{"; The harness model.", "[ggml-org/gpt-oss-120b-GGUF:MXFP4]"},
		{`"Recommended Sampling Parameters"`, "[ggml-org/gpt-oss-120b-GGUF:MXFP4]"},
		{"; The vision model;", "[ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0]"},
		{"; The audio model;", "[ggml-org/gemma-4-E4B-it-GGUF:Q8_0]"},
		{"; embeddings serves it as an embedding model.", "[ggml-org/embeddinggemma-2-GGUF:Q8_0]"},
	} {
		at := strings.Index(got, tt.phrase)
		section := strings.Index(got, tt.section)
		if at < 0 || section < 0 || at > section || strings.Contains(got[at:section], "\n[") {
			t.Errorf("%q does not render in the comment above %s", tt.phrase, tt.section)
		}
	}
}

// TestRenderPresetsImageBatch shows a model's image batch cap renders as llama.cpp's
// mtmd-batch-max-tokens in its own section, with its reason in the comment above it, and that a
// model without a cap renders no such key, leaving the server's default.
func TestRenderPresetsImageBatch(t *testing.T) {
	const vision = "ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0"
	got := render(t, strixHalo)
	for name, keys := range parseINI(t, got) {
		v, ok := keys["mtmd-batch-max-tokens"]
		switch {
		case name == vision && v != "512":
			t.Errorf("[%s] mtmd-batch-max-tokens = %q; want 512", name, v)
		case name != vision && ok:
			t.Errorf("[%s] mtmd-batch-max-tokens = %q; want the key absent", name, v)
		}
	}
	at := strings.Index(got, "; mtmd-batch-max-tokens = 512 encodes each image")
	section := strings.Index(got, "["+vision+"]")
	if at < 0 || section < 0 || at > section || strings.Contains(got[at:section], "\n[") {
		t.Errorf("the image batch cap's reason does not render in the comment above [%s]", vision)
	}
}

// TestLoadNamesInvalidImageBatch shows an image batch cap needs a positive value, its reason and
// a model with the vision role, and that a raw arg may not set it beside the typed field.
func TestLoadNamesInvalidImageBatch(t *testing.T) {
	base, err := os.ReadFile(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	const (
		value  = "mtmd_batch_max_tokens = 512\n"
		reason = "mtmd_batch_max_tokens_reason = \"\"\"\n"
	)
	text := string(base)
	cut := strings.Index(text, reason)
	end := strings.Index(text[cut+len(reason):], `"""`) + cut + len(reason) + len(`"""`)
	noReason := text[:cut] + text[end:]
	tests := []struct {
		name, text, want string
	}{
		{"zero", replace(t, text, value, "mtmd_batch_max_tokens = 0\n"),
			"models[1].mtmd_batch_max_tokens: required with its reason, a positive integer"},
		{"negative", replace(t, text, value, "mtmd_batch_max_tokens = -1\n"),
			"models[1].mtmd_batch_max_tokens: required with its reason, a positive integer"},
		{"no reason", noReason, "models[1].mtmd_batch_max_tokens_reason: required"},
		{"no vision role", replace(t, text, `roles = ["chat", "vision"]
card_url = "https://huggingface.co/google/gemma-4-26B-A4B-it"`, `roles = ["chat"]
card_url = "https://huggingface.co/google/gemma-4-26B-A4B-it"`),
			"models[1].mtmd_batch_max_tokens: only a model with the vision role encodes images"},
		{"raw arg", addLine(t, text, "ub =", "mtmd-batch-max-tokens = 512"),
			"models[3].args.mtmd-batch-max-tokens: rendered from the profile's typed fields"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertFails(t, writeProfile(t, tt.text), tt.want)
		})
	}
}

// TestLoadNamesMisplacedOriginReason shows only a model of the origin STANDARDS restricts records
// why it was chosen, and that such a model loads with or without the reason: admission judges
// its absence, not validation.
func TestLoadNamesMisplacedOriginReason(t *testing.T) {
	base, err := os.ReadFile(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	const us = "origin = \"United States\"\n"
	text := string(base)
	reason := strings.Replace(text, us, us+"origin_reason = \"No alternative serves.\"\n", 1)
	assertFails(t, writeProfile(t, reason),
		`models[0].origin_reason: only a model whose origin is China records why it was chosen, not one of "United States"`)

	for name, text := range map[string]string{
		"with a reason":    strings.Replace(reason, us, "origin = \"China\"\n", 1),
		"without a reason": strings.Replace(text, us, "origin = \"China\"\n", 1),
	} {
		t.Run(name, func(t *testing.T) {
			p, err := profile.Load(writeProfile(t, text))
			if err != nil {
				t.Fatal(err)
			}
			if !p.Models[0].OriginRestricted() {
				t.Errorf("origin %q not restricted", p.Models[0].Origin)
			}
		})
	}
}

// TestRenderPresetsRejectsInvalid shows a profile that fails validation does not render.
func TestRenderPresetsRejectsInvalid(t *testing.T) {
	p, err := profile.Load(skeleton)
	if err != nil {
		t.Fatal(err)
	}
	p.Models[0].CReason = ""
	var got bytes.Buffer
	err = p.RenderPresets(&got)
	if err == nil || !strings.Contains(err.Error(), "models[0].c_reason") {
		t.Errorf("RenderPresets(no c_reason) = %v; want an error naming models[0].c_reason", err)
	}
	if got.Len() != 0 {
		t.Errorf("RenderPresets wrote %d bytes for an invalid profile; want none", got.Len())
	}
}

// TestLoadNamesUndefinedKey adds a key the schema doesn't define, at each level, and expects
// the error to name it.
func TestLoadNamesUndefinedKey(t *testing.T) {
	base, err := os.ReadFile(skeleton)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		after, add string // the line added, after the first line starting with after
		key        string
	}{
		{"kv =", `service_user = "llama"`, "service_user"},
		{"backend =", `hf_home = "/srv/hf"`, "engine.hf_home"},
		{"gpu_layers =", "models_dir = 1", "defaults.models_dir"},
		{"card_url =", "temperature = 0.7", "models.temperature"},
		{"startup =", "budget_mib = 1", "sets.budget_mib"},
		{"min_p =", "typical_p = 0.9", "models.sampling.typical_p"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			path := writeProfile(t, addLine(t, string(base), tt.after, tt.add))
			assertFails(t, path, tt.key+": not a key the schema defines")
		})
	}
}

// TestLoadNamesSamplingOutOfRange sets each sampling value outside its range, or removes it.
func TestLoadNamesSamplingOutOfRange(t *testing.T) {
	base, err := os.ReadFile(skeleton)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		old, new string
		want     string
	}{
		{"temperature = 1.0", "temperature = -0.1", "models[0].sampling.temperature: -0.1 is below 0"},
		{"top_p = 1.0", "top_p = 1.5", "models[0].sampling.top_p: 1.5 is outside [0, 1]"},
		{"top_p = 1.0", "top_p = -0.5", "models[0].sampling.top_p: -0.5 is outside [0, 1]"},
		{"min_p = 0.0", "min_p = 2.0", "models[0].sampling.min_p: 2 is outside [0, 1]"},
		{"top_k = 0", "top_k = -1", "models[0].sampling.top_k: -1 is below 0"},
		{"top_k = 0", `reasoning = "max"`, `models[0].sampling.reasoning: "max" is not one of`},
		{"temperature = 1.0\n", "", "models[0].sampling.temperature: required"},
		{"top_p = 1.0\n", "", "models[0].sampling.top_p: required"},
		{"top_k = 0\n", "", "models[0].sampling.top_k: required"},
		{"min_p = 0.0\n", "", "models[0].sampling.min_p: required"},
		{`source = "https://github.com/openai/gpt-oss"`, "", "models[0].sampling.source: required"},
		{"[models.sampling]", "[models.unused]", "models[0].sampling: required for a generative model"},
		{`kind = "generative"`, `kind = "embedding"`, "models[0].sampling: an embedding model doesn't sample"},
		{`kind = "generative"`, `kind = "reranker"`, `models[0].kind: "reranker" is not one of`},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if !strings.Contains(string(base), tt.old) {
				t.Fatalf("skeleton has no %q", tt.old)
			}
			path := writeProfile(t, strings.Replace(string(base), tt.old, tt.new, 1))
			assertFails(t, path, tt.want)
		})
	}
}

// TestLoadPrompts shows the Strix Halo profile's card prompts load as written: gemma-4-E4B's ASR
// instruction and EmbeddingGemma 2's task prompts, each with its source, and none on a model
// whose card gives none.
func TestLoadPrompts(t *testing.T) {
	p, err := profile.Load(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	asr := p.Model("ggml-org/gemma-4-E4B-it-GGUF:Q8_0").Prompts
	if asr == nil || asr.ASR != "Transcribe the following speech segment in English into English text." ||
		asr.Source != "https://huggingface.co/google/gemma-4-E4B-it" || len(asr.Embedding) != 0 {
		t.Errorf("gemma-4-E4B's prompts %+v; want the card's ASR instruction alone, from its card", asr)
	}
	emb := p.Model("ggml-org/embeddinggemma-2-GGUF:Q8_0").Prompts
	if emb == nil || emb.Embedding["STS"] != "task: sentence similarity | query: " ||
		emb.Embedding["Retrieval-document"] != "title: none | text: " || len(emb.Embedding) != 14 || emb.ASR != "" {
		t.Errorf("EmbeddingGemma 2's prompts %+v; want its 14 task prompts alone", emb)
	}
	if got := p.Model("ggml-org/gpt-oss-120b-GGUF:MXFP4").Prompts; got != nil {
		t.Errorf("gpt-oss-120b's prompts %+v; want none", got)
	}
}

// TestValidateNamesInvalidPrompts changes the Strix Halo profile's prompts in code, each one
// breaking one rule, and expects the error that names it: a prompt belongs to a role the model
// declares, carries its source and reason, and isn't empty, and a set's audio model gives the
// ASR prompt its transcriptions send.
func TestValidateNamesInvalidPrompts(t *testing.T) {
	const (
		gptOSS    = "ggml-org/gpt-oss-120b-GGUF:MXFP4"
		e4b       = "ggml-org/gemma-4-E4B-it-GGUF:Q8_0"
		embedding = "ggml-org/embeddinggemma-2-GGUF:Q8_0"
	)
	card := func() *profile.Prompts {
		return &profile.Prompts{Source: "https://example.com/card", Reason: "The card's."}
	}
	for _, tt := range []struct {
		name string
		edit func(p *profile.Profile)
		want string
	}{
		{"an ASR prompt on a model without the audio role", func(p *profile.Profile) {
			pr := card()
			pr.ASR = "Transcribe this."
			p.Model(gptOSS).Prompts = pr
		}, "models[0].prompts.asr: only a model with the audio role transcribes"},
		{"embedding prompts on a model without the embedding role", func(p *profile.Profile) {
			pr := card()
			pr.Embedding = map[string]string{"query": "query: "}
			p.Model(gptOSS).Prompts = pr
		}, "models[0].prompts.embedding: only a model with the embedding role takes embedding prompts"},
		{"no prompt at all", func(p *profile.Profile) {
			p.Model(gptOSS).Prompts = card()
		}, "models[0].prompts: sets neither embedding nor asr"},
		{"no source", func(p *profile.Profile) {
			p.Model(embedding).Prompts.Source = ""
		}, "models[3].prompts.source: required"},
		{"no reason", func(p *profile.Profile) {
			p.Model(e4b).Prompts.Reason = ""
		}, "models[2].prompts.reason: required"},
		{"an empty prompt", func(p *profile.Profile) {
			p.Model(embedding).Prompts.Embedding["STS"] = ""
		}, "models[3].prompts.embedding.STS: empty"},
		{"a set's audio model without an ASR prompt", func(p *profile.Profile) {
			p.Model(e4b).Prompts = nil
		}, `sets[0].roles.audio: "ggml-org/gemma-4-E4B-it-GGUF:Q8_0" gives no prompts.asr to transcribe with`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, err := profile.Load(strixHalo)
			if err != nil {
				t.Fatal(err)
			}
			tt.edit(p)
			if err := p.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Validate = %v; want an error containing %q", err, tt.want)
			}
		})
	}

	base, err := os.ReadFile(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	text := addLine(t, string(base), "asr =", `instruction = "Transcribe this."`)
	assertFails(t, writeProfile(t, text), "models.prompts.instruction: not a key the schema defines")
}

// TestRenderPresetsSlots shows each model's slots render as parallel in its own section, each
// model's value its own, with its reason in the comment above, and none in [*], which keeps the
// host class's unified KV.
func TestRenderPresetsSlots(t *testing.T) {
	got := render(t, skeleton)
	ini := parseINI(t, got)
	if v, ok := ini["*"]["parallel"]; ok {
		t.Errorf("[*] parallel = %q; want the key absent, each model's section setting its own", v)
	}
	if v := ini["*"]["kv-unified"]; v != "true" {
		t.Errorf("[*] kv-unified = %q; want true", v)
	}
	for id, want := range map[string]string{
		"ggml-org/gpt-oss-120b-GGUF:MXFP4":    "4",
		"ggml-org/embeddinggemma-2-GGUF:Q8_0": "2",
	} {
		if v := ini[id]["parallel"]; v != want {
			t.Errorf("[%s] parallel = %q; want %s", id, v, want)
		}
	}
	at := strings.Index(got, "; 2 slots, so the render shows each model's own.")
	section := strings.Index(got, "[ggml-org/embeddinggemma-2-GGUF:Q8_0]")
	if at < 0 || section < 0 || at > section || strings.Contains(got[at:section], "\n[") {
		t.Errorf("the embedding model's slots reason does not render in the comment above its section")
	}
	// The router's own parallel and kv-unified keys, long or short, stay the typed fields'.
	base, err := os.ReadFile(skeleton)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"parallel", "np", "kv-unified", "kvu"} {
		text := addLine(t, string(base), "ub =", key+" = 4")
		assertFails(t, writeProfile(t, text), "models[1].args."+key+": rendered from the profile's typed fields")
	}
}

// TestRenderPresetsReasoning shows a reasoning effort, when one is set, renders as the chat
// template's reasoning_effort; set A sets none.
func TestRenderPresetsReasoning(t *testing.T) {
	base, err := os.ReadFile(skeleton)
	if err != nil {
		t.Fatal(err)
	}
	path := writeProfile(t, addLine(t, string(base), "min_p =", `reasoning = "high"`))
	want := `chat-template-kwargs = {"reasoning_effort":"high"}`
	if got := render(t, path); !strings.Contains(got, want) {
		t.Errorf("RenderPresets(reasoning high) lacks %q:\n%s", want, got)
	}
	if got := render(t, skeleton); strings.Contains(got, "chat-template-kwargs") {
		t.Errorf("RenderPresets(no reasoning) sets chat-template-kwargs:\n%s", got)
	}
}

// TestLoadNamesInvalidArgs shows a raw arg may not set a key the profile types or the unit or
// host owns, nor hold a value that isn't a scalar, and that args need a reason.
func TestLoadNamesInvalidArgs(t *testing.T) {
	base, err := os.ReadFile(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		add, want string
	}{
		{"c = 4096", "models[3].args.c: rendered from the profile's typed fields"},
		{"temp = 0.5", "models[3].args.temp: rendered from the profile's typed fields"},
		{"load-on-startup = false", "models[3].args.load-on-startup: rendered from the profile's typed fields"},
		{`hf-repo = "x/y:Q8_0"`, "models[3].args.hf-repo: rendered from the profile's typed fields"},
		{`hf = "x/y:Q8_0"`, "models[3].args.hf: rendered from the profile's typed fields"},
		{"lora = [1, 2]", "models[3].args.lora: a []interface {}, not a string, integer, float or boolean"},
	}
	for _, key := range []string{
		"host", "port", "models-dir", "models-preset", "models-max", "no-models-autoload",
		"slot-save-path", "log-file", "api-key-file", "ssl-key-file", "ssl-cert-file", "model",
		"mmproj", "chat-template-file",
	} {
		tests = append(tests, struct{ add, want string }{
			key + ` = "x"`, "models[3].args." + key + ": owned by the unit or the host, not the profile",
		})
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			path := writeProfile(t, addLine(t, string(base), "ub =", tt.add))
			assertFails(t, path, tt.want)
		})
	}

	p, err := profile.Load(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	p.Models[3].ArgsReason = ""
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "models[3].args_reason: required") {
		t.Errorf("Validate(args without a reason) = %v; want models[3].args_reason: required", err)
	}
}

// extraSet returns a set's TOML, to append to a profile: its name, whether it is the startup
// set, its models, its roles and a memory budget. Its first model takes the chat role and, when
// it holds more than one, its last model the embedding role.
func extraSet(name string, startup bool, models ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n[[sets]]\nname = %q\nstartup = %t\nmodels = [", name, startup)
	for i, id := range models {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", id)
	}
	b.WriteString("]\n\n[sets.roles]\n")
	if len(models) > 0 {
		fmt.Fprintf(&b, "chat = %q\n", models[0])
	}
	if len(models) > 1 {
		fmt.Fprintf(&b, "embedding = %q\n", models[len(models)-1])
	}
	b.WriteString("\n[sets.memory_budget]\npool_mib = 98304\nmeasured_mib = 70000\nbuild = \"b11565\"\n")
	return b.String()
}

// extraModel returns text with a copy of its first model appended under the router ID id: a
// chat model in no set.
func extraModel(t *testing.T, text, id string) string {
	t.Helper()
	start := strings.Index(text, "[[models]]")
	if start < 0 {
		t.Fatal("extraModel: no [[models]]")
	}
	model := text[start:]
	if end := strings.Index(model[1:], "[[models]]"); end >= 0 {
		model = model[:end+1]
	}
	idLine := model[strings.Index(model, "id = "):]
	idLine = idLine[:strings.Index(idLine, "\n")]
	return text + "\n" + strings.Replace(model, idLine, fmt.Sprintf("id = %q", id), 1)
}

// replace returns text with its one occurrence of old replaced by new.
func replace(t *testing.T, text, old, new string) string {
	t.Helper()
	if strings.Count(text, old) != 1 {
		t.Fatalf("replace: %q occurs %d times; want once", old, strings.Count(text, old))
	}
	return strings.Replace(text, old, new, 1)
}

func render(t *testing.T, path string) string {
	t.Helper()
	p, err := profile.Load(path)
	if err != nil {
		t.Fatalf("Load(%s): %v", path, err)
	}
	var b bytes.Buffer
	if err := p.RenderPresets(&b); err != nil {
		t.Fatalf("RenderPresets(%s): %v", path, err)
	}
	return b.String()
}

// parseINI reads presets text into its sections' keys and values. Comments, blank lines and
// order are ignored; a key set twice in a section, or outside one, fails the test.
func parseINI(t *testing.T, text string) map[string]map[string]string {
	t.Helper()
	sections := map[string]map[string]string{}
	var current map[string]string
	for n, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			name := line[1 : len(line)-1]
			if _, ok := sections[name]; ok {
				t.Fatalf("line %d: section [%s] repeated", n+1, name)
			}
			current = map[string]string{}
			sections[name] = current
		default:
			key, value, ok := strings.Cut(line, "=")
			if !ok || current == nil {
				t.Fatalf("line %d: %q is not a key in a section", n+1, line)
			}
			key = strings.TrimSpace(key)
			if _, ok := current[key]; ok {
				t.Fatalf("line %d: key %s repeated", n+1, key)
			}
			current[key] = strings.TrimSpace(value)
		}
	}
	return sections
}

// applied returns each model section of sections as the router applies it: the [*] section's
// keys, then the section's own over them. [*] itself is left out.
func applied(sections map[string]map[string]string) map[string]map[string]string {
	out := map[string]map[string]string{}
	for name, keys := range sections {
		if name == "*" {
			continue
		}
		merged := maps.Clone(sections["*"])
		if merged == nil {
			merged = map[string]string{}
		}
		maps.Copy(merged, keys)
		out[name] = merged
	}
	return out
}

// sameValue reports whether two presets values are equal, as numbers when both are: llama.cpp
// reads temp = 1 and temp = 1.0 alike.
func sameValue(a, b string) bool {
	x, errA := strconv.ParseFloat(a, 64)
	y, errB := strconv.ParseFloat(b, 64)
	if errA == nil && errB == nil {
		return x == y
	}
	return a == b
}

// addLine inserts line after the first line of text that starts with prefix.
func addLine(t *testing.T, text, prefix, line string) string {
	t.Helper()
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return strings.Join(slices.Insert(lines, i+1, line), "\n")
		}
	}
	t.Fatalf("addLine: no line starts with %q", prefix)
	return ""
}

func assertFails(t *testing.T, path string, want ...string) {
	t.Helper()
	_, err := profile.Load(path)
	if err == nil {
		t.Fatalf("Load = nil error; want one naming %q", want)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("Load error = %v; want it to contain %q", err, w)
		}
	}
}

func writeProfile(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "profile.toml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// dropLine removes the first line of text that starts with prefix.
func dropLine(t *testing.T, text, prefix string) string {
	t.Helper()
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return strings.Join(slices.Delete(lines, i, i+1), "\n")
		}
	}
	t.Fatalf("dropLine: no line starts with %q", prefix)
	return ""
}

// dropTable removes a table header and its keys, up to the next blank line.
func dropTable(t *testing.T, text, header string) string {
	t.Helper()
	start := strings.Index(text, header)
	if start < 0 {
		t.Fatalf("dropTable: no %s", header)
	}
	end := strings.Index(text[start:], "\n\n")
	return text[:start] + text[start+end+2:]
}

// mistral is the Mistral Small 4 candidate's router ID, as the Strix Halo profile names it.
const mistral = "unsloth/Mistral-Small-4-119B-2603-GGUF:Q4_K_XL"

// TestRouterTag shows the tag the router lists a section by: the quant type at the end of the
// section's tag, upper-cased.
func TestRouterTag(t *testing.T) {
	for _, tt := range []struct{ tag, want string }{
		{"UD-Q4_K_XL", "Q4_K_XL"},
		{"Q4_K_XL", "Q4_K_XL"},
		{"q8_0", "Q8_0"},
		{"MXFP4", "MXFP4"},
		{"NVFP4", "NVFP4"},
		{"model.Q4_0", "Q4_0"},
	} {
		if got := profile.RouterTag(tt.tag); got != tt.want {
			t.Errorf("RouterTag(%q) = %q; want %q", tt.tag, got, tt.want)
		}
	}
}

// TestLoadNamesNonCanonicalID shows a model ID whose tag the router would list differently is
// refused, with the ID the router would list it by.
func TestLoadNamesNonCanonicalID(t *testing.T) {
	base, err := os.ReadFile(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	ud := "unsloth/Mistral-Small-4-119B-2603-GGUF:UD-Q4_K_XL"
	path := writeProfile(t, strings.ReplaceAll(string(base), mistral, ud))
	assertFails(t, path, fmt.Sprintf("%q lists on the router as %q", ud, mistral))
}

// TestLoadConsumerReasoning shows a chat model's consumer reasoning setting decodes as written,
// OpenCode's nested options included though the schema leaves them free-form, and a model that
// sets none decodes to the zero value.
func TestLoadConsumerReasoning(t *testing.T) {
	base, err := os.ReadFile(skeleton)
	if err != nil {
		t.Fatal(err)
	}
	text := string(base)
	second := strings.LastIndex(text, "[[models]]")
	text = text[:second] + `[models.consumers]
pi = "medium"
reason = "the template accepts both"

[models.consumers.opencode]
reasoningEffort = "high"

[models.consumers.opencode.chat_template_kwargs]
enable_thinking = true

` + text[second:]
	p, err := profile.Load(writeProfile(t, text))
	if err != nil {
		t.Fatal(err)
	}
	c := p.Model("ggml-org/gpt-oss-120b-GGUF:MXFP4").Consumers
	kwargs, _ := c.OpenCode["chat_template_kwargs"].(map[string]any)
	if c.Pi != "medium" || c.OpenCode["reasoningEffort"] != "high" || kwargs["enable_thinking"] != true || len(c.OpenCode) != 2 {
		t.Errorf("Consumers = %+v; want Pi medium, OpenCode reasoningEffort high and chat_template_kwargs enable_thinking true", c)
	}
	if e := p.Model("ggml-org/embeddinggemma-2-GGUF:Q8_0").Consumers; e.Pi != "" || e.OpenCode != nil || e.Reason != "" {
		t.Errorf("the embedding model's Consumers = %+v; want none", e)
	}
	if p.Model("example/unlisted-GGUF:Q4_0") != nil {
		t.Error("Model(an unlisted ID) is not nil")
	}
}

// TestLoadMemoryBudgetEstimate shows a set's budget may be an estimate alone, with no build, but
// must be a measurement or an estimate, each positive, and a measurement names its build.
func TestLoadMemoryBudgetEstimate(t *testing.T) {
	base, err := os.ReadFile(skeleton)
	if err != nil {
		t.Fatal(err)
	}
	text := string(base)
	estimate := replace(t, dropLine(t, text, "build ="), "measured_mib = ", "estimated_mib = ")
	if _, err := profile.Load(writeProfile(t, estimate)); err != nil {
		t.Errorf("Load(an estimate alone, no build): %v", err)
	}
	both := addLine(t, text, "measured_mib =", "estimated_mib = 70000")
	if _, err := profile.Load(writeProfile(t, both)); err != nil {
		t.Errorf("Load(a measurement beside its estimate): %v", err)
	}
	for _, tt := range []struct{ name, text, want string }{
		{"neither", dropLine(t, text, "measured_mib ="), "sets[0].memory_budget.measured_mib: required, or estimated_mib"},
		{"a negative estimate", replace(t, estimate, "estimated_mib = ", "estimated_mib = -"), "sets[0].memory_budget.estimated_mib: required, a positive integer"},
		{"a measurement without its build", dropLine(t, both, "build ="), "sets[0].memory_budget.build: required"},
		{"an estimate naming a malformed build", addLine(t, estimate, "estimated_mib =", `build = "latest"`), `sets[0].memory_budget.build: "latest" is not a llama.cpp build`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assertFails(t, writeProfile(t, tt.text), tt.want)
		})
	}
}

// TestLoadNamesInvalidConsumers shows a model's consumer reasoning must give a Pi thinking level
// Pi takes and a reason, must set Pi or OpenCode, and belongs only to a generative chat model;
// a key outside the schema is refused, though OpenCode's options are free-form.
func TestLoadNamesInvalidConsumers(t *testing.T) {
	base, err := os.ReadFile(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	text := string(base)
	const mistralPi = "[models.consumers]\npi = \"high\""
	for _, tt := range []struct{ name, text, want string }{
		{"a level Pi doesn't take", replace(t, text, mistralPi, "[models.consumers]\npi = \"maximum\""), `models[4].consumers.pi: "maximum" is not one of Pi's thinking levels`},
		{"no reason", replace(t, text, "reason = \"\"\"\nMistral Small 4's template", "unused = \"\"\"\nMistral Small 4's template"), "models[4].consumers.reason: required"},
		{"a reason alone", replace(t, replace(t, text, mistralPi+"\n", "[models.consumers]\n"), "opencode = { reasoningEffort = \"high\" }\n", ""), "models[4].consumers: sets neither pi nor opencode"},
		{"on the embedding model", replace(t, text, "[models.args]\nembeddings = true", "[models.consumers]\npi = \"high\"\nreason = \"x\"\n\n[models.args]\nembeddings = true"), "models[3].consumers: only a generative chat model runs under a consumer"},
		{"an undefined key", replace(t, text, mistralPi, mistralPi+"\nclaude = \"high\""), "models.consumers.claude: not a key the schema defines"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assertFails(t, writeProfile(t, tt.text), tt.want)
		})
	}

	p, err := profile.Load(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	p.Model(mistral).Consumers.Reason = ""
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "models[4].consumers.reason: required") {
		t.Errorf("Validate(consumers without a reason) = %v; want models[4].consumers.reason: required", err)
	}
}

// TestPresetSection shows a model's preset section is the [*] defaults and its own section, key
// lines alone, the same bytes on every call; that a change to a key it runs at changes them, and
// a change to a reason does not; and that another model's section, load-on-startup and the
// comments are left out.
func TestPresetSection(t *testing.T) {
	const chat, embed = "ggml-org/gpt-oss-120b-GGUF:MXFP4", "ggml-org/embeddinggemma-2-GGUF:Q8_0"
	p, err := profile.Load(skeleton)
	if err != nil {
		t.Fatal(err)
	}
	section := func() string {
		t.Helper()
		b, err := p.PresetSection(chat)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	got := section()
	if !strings.HasPrefix(got, "[*]\njinja = true\n") || !strings.Contains(got, "\n["+chat+"]\nhf-repo = "+chat+"\nc = 131072\n") {
		t.Errorf("PresetSection(%s) =\n%s\nwant [*] then the model's section, key lines alone", chat, got)
	}
	for _, absent := range []string{";", "load-on-startup", "[" + embed + "]"} {
		if strings.Contains(got, absent) {
			t.Errorf("PresetSection(%s) holds %q:\n%s", chat, absent, got)
		}
	}
	if again := section(); again != got {
		t.Errorf("a second PresetSection differs:\n%s\nwant\n%s", again, got)
	}

	m := p.Model(chat)
	m.CReason += " A reason changed."
	if s := section(); s != got {
		t.Errorf("a changed reason changed the section:\n%s", s)
	}
	m.C /= 2
	if s := section(); s == got || !strings.Contains(s, "\nc = 65536\n") {
		t.Errorf("a changed c left the section\n%s", s)
	}
	m.C *= 2
	p.Defaults.C /= 2
	if s := section(); s == got {
		t.Errorf("a changed default left the section\n%s", s)
	}

	if b, err := p.PresetSection("example/none-GGUF:Q4_0"); err == nil {
		t.Errorf("PresetSection(unknown) = %q; want an error", b)
	}
}

// TestOverride shows Override renders one model's section at the slots and c it is given, every
// other section as the profile's own, leaves the profile itself unchanged, and refuses a c over
// the card's context or a model the profile doesn't hold.
func TestOverride(t *testing.T) {
	p, err := profile.Load(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	const id = "ggml-org/gemma-4-E4B-it-GGUF:Q8_0"
	committed := parseINI(t, render(t, strixHalo))
	o, err := p.Override(id, 8, 15648)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := o.RenderPresets(&b); err != nil {
		t.Fatal(err)
	}
	got := parseINI(t, b.String())
	if got[id]["parallel"] != "8" || got[id]["c"] != "15648" {
		t.Errorf("[%s] parallel = %q, c = %q; want 8 and 15648", id, got[id]["parallel"], got[id]["c"])
	}
	for name, keys := range committed {
		want := keys
		if name == id {
			want = maps.Clone(keys)
			want["parallel"], want["c"] = "8", "15648"
		}
		if !maps.Equal(got[name], want) {
			t.Errorf("[%s] %v; want %v", name, got[name], want)
		}
	}
	if len(got) != len(committed) {
		t.Errorf("%d sections; want the profile's %d", len(got), len(committed))
	}
	if m := p.Model(id); m.Slots != 4 || m.C != 32768 {
		t.Errorf("the profile's own %s is at %d slots and c = %d after Override; want it unchanged", id, m.Slots, m.C)
	}
	if _, err := p.Override(id, 2, 262144); err == nil || !strings.Contains(err.Error(), "over the card's context") {
		t.Errorf("Override over the card's context = %v; want refused", err)
	}
	if _, err := p.Override("example/none-GGUF:Q4_0", 2, 4096); err == nil {
		t.Error("Override of a model the profile doesn't hold: no error")
	}
}
