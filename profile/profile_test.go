package profile_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/profile"
)

// update rewrites the golden files from the current render: go test ./profile -update.
var update = flag.Bool("update", false, "rewrite golden files")

const skeleton = "testdata/skeleton.toml"

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

// TestRenderPresets compares the skeleton's rendered presets with its golden file.
func TestRenderPresets(t *testing.T) {
	p, err := profile.Load(skeleton)
	if err != nil {
		t.Fatal(err)
	}
	var got bytes.Buffer
	if err := p.RenderPresets(&got); err != nil {
		t.Fatalf("RenderPresets: %v", err)
	}
	golden := "testdata/skeleton.ini"
	if *update {
		if err := os.WriteFile(golden, got.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != string(want) {
		t.Errorf("RenderPresets differs from %s (rerun with -update to accept):\n--- got\n%s\n--- want\n%s", golden, got.String(), want)
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
