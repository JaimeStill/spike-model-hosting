package profile

import (
	_ "embed"
	"fmt"
	"io"
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
	Funcs(template.FuncMap{"comment": comment}).
	Parse(presetsTemplate))

// RenderPresets writes the profile's llama.cpp router presets to w. Each model's reasons render
// as a comment above its section. The profile is validated first, so a profile built in code
// renders only when Load would have accepted it.
func (p *Profile) RenderPresets(w io.Writer) error {
	if err := p.Validate(); err != nil {
		return fmt.Errorf("profile: %w", err)
	}
	if err := presets.Execute(w, p); err != nil {
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
		line := ";"
		for word := range strings.FieldsSeq(para) {
			if len(line) > 1 && len(line)+1+len(word) > commentWidth {
				b.WriteString(line + "\n")
				line = ";"
			}
			line += " " + word
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}
