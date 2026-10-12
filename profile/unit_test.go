package profile_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/profile"
)

// fakeHost is the render-time host the unit tests render with. Its values are made up: no real
// host's values are committed.
var fakeHost = profile.Host{ServiceUser: "svc", HFHome: "/srv/hf", ModelsDir: "/srv/models"}

// TestRenderUnit compares the Strix Halo profile's unit, rendered with fakeHost, with its golden
// file.
func TestRenderUnit(t *testing.T) {
	const golden = "testdata/strix-halo.service"
	got := renderUnit(t, strixHalo, fakeHost)
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("RenderUnit differs from %s (rerun with -update to accept):\n--- got\n%s\n--- want\n%s", golden, got, want)
	}
}

// TestRenderUnitTailnet shows a tailnet unit runs the engine through its current link in router
// mode with the installed presets, takes each host value, resolves its address when it starts
// rather than naming one, and starts after tailscaled.
func TestRenderUnitTailnet(t *testing.T) {
	got := renderUnit(t, strixHalo, fakeHost)
	for _, want := range []string{
		"User=svc\n",
		"Environment=HF_HOME=/srv/hf\n",
		"exec /opt/llama.cpp/current/llama-server --models-dir /srv/models ",
		"--host $(tailscale ip -4) --port 8080 ",
		"--models-preset /etc/llama-router/models.ini'\n",
		"After=network-online.target tailscaled.service\n",
		"Requires=tailscaled.service\n",
		"Restart=on-failure\n",
		"RestartSec=5\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderUnit lacks %q:\n%s", want, got)
		}
	}
	// Router mode: no model is named on the command line.
	for _, flag := range []string{" -m ", " --model ", " -hf ", " --hf-repo "} {
		if strings.Contains(got, flag) {
			t.Errorf("RenderUnit names a model with %q; the router takes its models from the presets:\n%s", flag, got)
		}
	}
}

// TestRenderUnitLoopback shows a loopback profile's unit binds to 127.0.0.1 and doesn't wait on
// tailscaled.
func TestRenderUnitLoopback(t *testing.T) {
	base, err := os.ReadFile(skeleton)
	if err != nil {
		t.Fatal(err)
	}
	path := writeProfile(t, strings.Replace(string(base), `bind = "tailnet"`, `bind = "loopback"`, 1))
	got := renderUnit(t, path, fakeHost)
	if !strings.Contains(got, "--host 127.0.0.1 --port 8080 ") {
		t.Errorf("RenderUnit(loopback) does not bind 127.0.0.1:\n%s", got)
	}
	if strings.Contains(got, "tailscale") {
		t.Errorf("RenderUnit(loopback) names tailscale:\n%s", got)
	}
}

// TestRenderUnitRequiresHost shows the unit fails to render, and writes nothing, without each
// host value.
func TestRenderUnitRequiresHost(t *testing.T) {
	p, err := profile.Load(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		host profile.Host
		want string
	}{
		{profile.Host{HFHome: fakeHost.HFHome, ModelsDir: fakeHost.ModelsDir}, "host service user: required"},
		{profile.Host{ServiceUser: fakeHost.ServiceUser, ModelsDir: fakeHost.ModelsDir}, "host HF_HOME: required"},
		{profile.Host{ServiceUser: fakeHost.ServiceUser, HFHome: fakeHost.HFHome}, "host models directory: required"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			var got bytes.Buffer
			err := p.RenderUnit(&got, tt.host)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("RenderUnit(%+v) = %v; want an error containing %q", tt.host, err, tt.want)
			}
			if got.Len() != 0 {
				t.Errorf("RenderUnit wrote %d bytes without a host value; want none", got.Len())
			}
		})
	}
}

// TestRenderUnitRejectsInvalid shows a profile that fails validation renders no unit.
func TestRenderUnitRejectsInvalid(t *testing.T) {
	p, err := profile.Load(skeleton)
	if err != nil {
		t.Fatal(err)
	}
	p.Bind = ""
	var got bytes.Buffer
	if err := p.RenderUnit(&got, fakeHost); err == nil || !strings.Contains(err.Error(), "bind: required") {
		t.Errorf("RenderUnit(no bind) = %v; want an error naming bind", err)
	}
	if got.Len() != 0 {
		t.Errorf("RenderUnit wrote %d bytes for an invalid profile; want none", got.Len())
	}
}

// TestHostValidateRefusesUnsafe shows a host value the unit couldn't carry as given fails, named.
func TestHostValidateRefusesUnsafe(t *testing.T) {
	tests := []struct {
		host profile.Host
		want string
	}{
		{profile.Host{ServiceUser: "svc user", HFHome: "/srv/hf", ModelsDir: "/srv/models"}, "host service user:"},
		{profile.Host{ServiceUser: "svc", HFHome: "/srv/$HOME", ModelsDir: "/srv/models"}, "host HF_HOME:"},
		{profile.Host{ServiceUser: "svc", HFHome: "/srv/hf", ModelsDir: "/srv/it's"}, "host models directory:"},
		{profile.Host{ServiceUser: "svc", HFHome: "/srv/100%", ModelsDir: "/srv/models"}, "host HF_HOME:"},
		{profile.Host{ServiceUser: "svc", HFHome: "srv/hf", ModelsDir: "/srv/models"}, "host HF_HOME: \"srv/hf\" is not an absolute path"},
		{profile.Host{ServiceUser: "svc", HFHome: "/srv/hf", ModelsDir: "models"}, "host models directory: \"models\" is not an absolute path"},
	}
	for _, tt := range tests {
		err := tt.host.Validate()
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("Validate(%+v) = %v; want an error containing %q", tt.host, err, tt.want)
		}
	}
}

// TestEngineRepository shows the engine a profile names has its upstream repository.
func TestEngineRepository(t *testing.T) {
	p, err := profile.Load(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Engine.Repository(); got != "ggml-org/llama.cpp" {
		t.Errorf("Engine.Repository() = %q; want ggml-org/llama.cpp", got)
	}
}

func renderUnit(t *testing.T, path string, h profile.Host) string {
	t.Helper()
	p, err := profile.Load(path)
	if err != nil {
		t.Fatalf("Load(%s): %v", path, err)
	}
	var b bytes.Buffer
	if err := p.RenderUnit(&b, h); err != nil {
		t.Fatalf("RenderUnit(%s): %v", path, err)
	}
	return b.String()
}
