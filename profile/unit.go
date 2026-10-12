package profile

import (
	_ "embed"
	"fmt"
	"io"
	"text/template"
)

// The router's install layout, which every unit renders the same: the engine's build in
// /opt/llama.cpp/<tag> behind a current link, the rendered presets at their installed path, and
// the port clients reach the router on.
const (
	EngineBinary = "/opt/llama.cpp/current/llama-server"
	PresetsPath  = "/etc/llama-router/models.ini"
	RouterPort   = 8080
)

// tailnetAddress is the shell substitution a tailnet unit binds to: the host's tailnet IPv4,
// resolved when the unit starts, never written into the render.
const tailnetAddress = "$(tailscale ip -4)"

// loopbackAddress is the address a loopback unit binds to.
const loopbackAddress = "127.0.0.1"

// unitTemplate renders the llama.cpp router's systemd unit.
//
//go:embed unit.service.tmpl
var unitTemplate string

var unit = template.Must(template.New("unit").Parse(unitTemplate))

// RenderUnit writes the llama.cpp router's systemd unit to w: the engine, through its current
// link, in router mode with the presets at PresetsPath, bound per the profile's Bind, and run
// with the host's values. The profile and the host are validated first, so a unit with a blank
// in it never renders.
func (p *Profile) RenderUnit(w io.Writer, h Host) error {
	if err := p.Validate(); err != nil {
		return fmt.Errorf("profile: %w", err)
	}
	if err := h.Validate(); err != nil {
		return fmt.Errorf("profile: %w", err)
	}
	address := tailnetAddress
	if p.Bind == BindLoopback {
		address = loopbackAddress
	}
	data := struct {
		Profile *Profile
		Host    Host
		Engine  string
		Address string
		Port    int
		Presets string
	}{p, h, EngineBinary, address, RouterPort, PresetsPath}
	if err := unit.Execute(w, data); err != nil {
		return fmt.Errorf("profile: render unit: %w", err)
	}
	return nil
}
