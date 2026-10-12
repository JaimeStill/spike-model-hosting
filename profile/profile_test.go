package profile_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/profile"
)

// update rewrites the golden files from the current render: go test ./profile -update.
var update = flag.Bool("update", false, "rewrite golden files")

const (
	skeleton  = "testdata/skeleton.toml"
	strixHalo = "../profiles/strix-halo.toml"
	// pinned is personal-agents' 96GiB preset, copied at the commit testdata/README.md names.
	pinned = "testdata/unified-96gb.ini"
)

func TestLoadAcceptsComplete(t *testing.T) {
	p, err := profile.Load(skeleton)
	if err != nil {
		t.Fatalf("Load(%s): %v", skeleton, err)
	}
	if p.HostClass != profile.HostStrixHalo || len(p.Models) != 1 {
		t.Errorf("Load(%s) = host class %q, %d models; want %q, 1", skeleton, p.HostClass, len(p.Models), profile.HostStrixHalo)
	}
}

// TestLoadNamesMissingField removes one required key at a time from the complete profile, and
// expects the error to name it by its TOML key path.
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
		{"slots =", "slots"},
		{"kv =", "kv"},
		{"name =", "engine.name"},
		{"backend =", "engine.backend"},
		{"min_build =", "engine.min_build"},
		{"gpu_layers =", "defaults.gpu_layers"},
		{"checkpoint_min_step =", "defaults.checkpoint_min_step"},
		{"pool_mib =", "memory_budget.pool_mib"},
		{"measured_mib =", "memory_budget.measured_mib"},
		{"build =", "memory_budget.build"},
		{"id =", "models[0].id"},
		{"publisher =", "models[0].publisher"},
		{"origin =", "models[0].origin"},
		{"card_url =", "models[0].card_url"},
		{"c = 131072", "models[0].c"},
		{"c = 32768", "defaults.c"},
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
	t.Run("memory_budget", func(t *testing.T) {
		path := writeProfile(t, dropTable(t, string(base), "[memory_budget]"))
		assertFails(t, path, "memory_budget.pool_mib: required", "memory_budget.build: required")
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
		{`slots = 4`, `slots = -1`, "slots"},
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

// TestStrixHaloMatchesPinnedPreset renders the Strix Halo profile and compares its sections,
// keys and values with the pinned copy of personal-agents' 96GiB preset. The one difference
// allowed is load-on-startup = true on each model's section, which the profile renders and the
// pinned copy lacks.
func TestStrixHaloMatchesPinnedPreset(t *testing.T) {
	got := parseINI(t, render(t, strixHalo))
	text, err := os.ReadFile(pinned)
	if err != nil {
		t.Fatal(err)
	}
	want := parseINI(t, string(text))

	models := 0
	for name, keys := range got {
		if name == "*" {
			continue
		}
		models++
		if keys["load-on-startup"] != "true" {
			t.Errorf("[%s] load-on-startup = %q; want true", name, keys["load-on-startup"])
		}
		delete(keys, "load-on-startup")
	}
	if models != 4 {
		t.Errorf("rendered %d model sections; want 4", models)
	}
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

// dropLine removes the one line of text that starts with prefix.
func dropLine(t *testing.T, text, prefix string) string {
	t.Helper()
	lines := strings.Split(text, "\n")
	var kept []string
	for _, l := range lines {
		if !strings.HasPrefix(l, prefix) {
			kept = append(kept, l)
		}
	}
	if len(kept) != len(lines)-1 {
		t.Fatalf("dropLine(%q) removed %d lines; want 1", prefix, len(lines)-len(kept))
	}
	return strings.Join(kept, "\n")
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
