package profile

import (
	_ "embed"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/template"
)

// presetsTemplate renders the llama.cpp router's --models-preset INI file: a [*] section of
// shared defaults, then one section per model, named by its router ID.
//
//go:embed presets.ini.tmpl
var presetsTemplate string

// commentWidth is the column a rendered comment wraps before.
const commentWidth = 100

var presets = template.Must(template.New("presets").
	Funcs(template.FuncMap{"comment": comment, "join": strings.Join}).
	Parse(presetsTemplate))

// presetsData is what the presets template renders: the profile, and the router IDs of its
// startup set's models, the ones whose sections set load-on-startup.
type presetsData struct {
	*Profile
	Startup map[string]bool
}

// RenderPresets writes the profile's llama.cpp router presets to w. Each model's reasons render as
// a comment above its section. Only the startup set's models render load-on-startup = true; the
// rest render without the key, which the router reads as false. The profile is validated first, so
// a profile built in code renders only when Load would have accepted it.
func (p *Profile) RenderPresets(w io.Writer) error {
	if err := p.Validate(); err != nil {
		return fmt.Errorf("profile: %w", err)
	}
	startup := map[string]bool{}
	for _, id := range p.StartupSet().Models {
		startup[id] = true
	}
	if err := presets.Execute(w, presetsData{p, startup}); err != nil {
		return fmt.Errorf("profile: render presets: %w", err)
	}
	return nil
}

// comment renders text as INI comment lines, each starting "; " and wrapped before
// commentWidth. A blank line in text separates paragraphs, and renders as a bare ";".
func comment(text string) string {
	var b strings.Builder
	for i, para := range strings.Split(strings.TrimSpace(text), "\n\n") {
		if i > 0 {
			b.WriteString(";\n")
		}
		var line []string
		width := len(";")
		for word := range strings.FieldsSeq(para) {
			if len(line) > 0 && width+1+len(word) > commentWidth {
				fmt.Fprintf(&b, "; %s\n", strings.Join(line, " "))
				line, width = nil, len(";")
			}
			line = append(line, word)
			width += 1 + len(word)
		}
		fmt.Fprintf(&b, "; %s\n", strings.Join(line, " "))
	}
	return b.String()
}

// PresetSection returns the preset model id runs at, as the router applies it: the rendered [*]
// section of shared defaults, then the model's own section, each under its header and holding
// its key lines alone, in the order they render. A section's comments, the reasons, are left
// out, and so is load-on-startup, which says when the model loads, not how it runs. The bytes
// are the same for the same profile on every call: evaluate keys a model's results by their
// SHA-256, so a change to a key the model runs at, its own or a shared default, changes them, and
// a change to a reason, or to which set the router starts with, does not.
func (p *Profile) PresetSection(id string) ([]byte, error) {
	if p.Model(id) == nil {
		return nil, fmt.Errorf("profile: no model %q", id)
	}
	var rendered strings.Builder
	if err := p.RenderPresets(&rendered); err != nil {
		return nil, err
	}
	var out strings.Builder
	section, found := "", false
	for line := range strings.Lines(rendered.String()) {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || strings.HasPrefix(line, ";"):
			continue
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			section = line[1 : len(line)-1]
			if section == "*" || section == id {
				found = found || section == id
				out.WriteString(line + "\n")
			}
			continue
		}
		key, _, _ := strings.Cut(line, "=")
		if (section == "*" || section == id) && strings.TrimSpace(key) != "load-on-startup" {
			out.WriteString(line + "\n")
		}
	}
	if !found {
		return nil, fmt.Errorf("profile: the presets render no section for %s", id)
	}
	return []byte(out.String()), nil
}

// Override returns a copy of the profile with the model id at slots and context c, validated as
// Load validates a profile, so c may not pass the model's card context. The copy's RenderPresets
// is the render with that override: the model's section renders parallel = slots and c = c, and
// every other section, [*] among them, renders as the profile's own. Its reasons stay the
// profile's. evaluate's sweep renders each step this way; the profile itself is left unchanged.
func (p *Profile) Override(id string, slots, c int) (*Profile, error) {
	if p.Model(id) == nil {
		return nil, fmt.Errorf("profile: no model %q", id)
	}
	out := *p
	out.Models = slices.Clone(p.Models)
	m := out.Model(id)
	m.Slots, m.C = slots, c
	if err := out.Validate(); err != nil {
		return nil, fmt.Errorf("profile: %s at %d slots and c = %d: %w", id, slots, c, err)
	}
	return &out, nil
}
