package gate

import (
	"errors"
	"fmt"

	"github.com/BurntSushi/toml"
)

// Target is one role's per-session target: the largest request the role's committed tier lists
// send, its prompt (In) and the answer it is given room for (Out), in tokens, which each of a
// model's sessions must fit in the model's context at once. Source says how it was measured.
type Target struct {
	In     int    `toml:"in"`
	Out    int    `toml:"out"`
	Source string `toml:"source"`
}

// Tokens is the context one session at the target holds: its prompt and its answer.
func (t Target) Tokens() int { return t.In + t.Out }

// Targets are the per-session targets the sweep sizes a model's context by, one a role. They are
// data, measured once from the committed tier lists and read from evaluation/targets.toml, which
// LoadTargets reads; the file says how each was measured.
type Targets struct {
	Agent     Target `toml:"agent"`
	Vision    Target `toml:"vision"`
	Audio     Target `toml:"audio"`
	Embedding Target `toml:"embedding"`
}

// Of returns role's target; the zero Target for a role Targets doesn't hold.
func (t Targets) Of(role string) Target {
	return map[string]Target{RoleAgent: t.Agent, RoleVision: t.Vision, RoleAudio: t.Audio, RoleEmbedding: t.Embedding}[role]
}

// LoadTargets reads the targets file at path. Each role's prompt is required and positive, its
// answer at least 0 (an embedding has none) and its source recorded, and a key the file's schema
// doesn't define fails, as a profile's does.
func LoadTargets(path string) (Targets, error) {
	var t Targets
	md, err := toml.DecodeFile(path, &t)
	if err != nil {
		return t, fmt.Errorf("targets: %w", err)
	}
	var errs []error
	for _, k := range md.Undecoded() {
		errs = append(errs, fmt.Errorf("%s: not a key the schema defines", k))
	}
	for _, role := range Roles {
		v := t.Of(role)
		switch {
		case v.In < 1:
			errs = append(errs, fmt.Errorf("%s.in: required, a positive number of tokens", role))
		case v.Out < 0:
			errs = append(errs, fmt.Errorf("%s.out: at least 0 tokens", role))
		case v.Source == "":
			errs = append(errs, fmt.Errorf("%s.source: required, how the target was measured", role))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return t, fmt.Errorf("targets %s: %w", path, err)
	}
	return t, nil
}
