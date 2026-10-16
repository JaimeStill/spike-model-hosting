package gate

import (
	"errors"
	"fmt"
	"slices"

	"github.com/BurntSushi/toml"

	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
)

// The roles evaluate judges a model in. Agent is the profile's chat role: the set's chat model,
// the one every coding agent's session runs on; the others are the profile's roles of the same
// names.
const (
	RoleAgent     = "agent"
	RoleVision    = "vision"
	RoleAudio     = "audio"
	RoleEmbedding = "embedding"
)

// Roles are the roles in the order a set assigns them.
var Roles = []string{RoleAgent, RoleVision, RoleAudio, RoleEmbedding}

// ProfileRole returns the profile's role that role names: profile.RoleChat for RoleAgent, and
// the role itself for the rest.
func ProfileRole(role string) string {
	if role == RoleAgent {
		return profile.RoleChat
	}
	return role
}

// ParseRole returns the role name names, accepting the profile's chat for RoleAgent, and fails
// for a name that is neither.
func ParseRole(name string) (string, error) {
	if name == profile.RoleChat {
		return RoleAgent, nil
	}
	if slices.Contains(Roles, name) {
		return name, nil
	}
	return "", fmt.Errorf("role %q is not one of %q (or chat, the agent role)", name, Roles)
}

// Floors are the absolute floors fitness and capacity judge a model against, per role, and the
// pool's free memory every stage holds a set to. They are data, read from evaluation/floors.toml.
type Floors struct {
	Pool struct {
		// FreeMiB is the pool's least free memory, idle and at the peak of 4 in flight.
		FreeMiB float64 `toml:"free_mib"`
	} `toml:"pool"`
	Agent struct {
		DecodeTokS        float64 `toml:"decode_tok_s"`
		PrefillTokS       float64 `toml:"prefill_tok_s"`
		TTFT8kS           float64 `toml:"ttft_8k_s"`
		Stream4DecodeTokS float64 `toml:"stream4_decode_tok_s"`
	} `toml:"agent"`
	Vision struct {
		ImageAnswerS float64 `toml:"image_answer_s"`
	} `toml:"vision"`
	Audio struct {
		RTFx        float64 `toml:"rtfx"`
		Stream4RTFx float64 `toml:"stream4_rtfx"`
	} `toml:"audio"`
	Embedding struct {
		ThroughputTokS float64 `toml:"throughput_tok_s"`
		Query32MS      float64 `toml:"query32_ms"`
	} `toml:"embedding"`
}

// LoadFloors reads the floors file at path. Every floor is required and positive, and a key the
// file's schema doesn't define fails, as a profile's does.
func LoadFloors(path string) (Floors, error) {
	var f Floors
	md, err := toml.DecodeFile(path, &f)
	if err != nil {
		return f, fmt.Errorf("floors: %w", err)
	}
	var errs []error
	for _, k := range md.Undecoded() {
		errs = append(errs, fmt.Errorf("%s: not a key the schema defines", k))
	}
	errs = append(errs, f.Validate())
	if err := errors.Join(errs...); err != nil {
		return f, fmt.Errorf("floors %s: %w", path, err)
	}
	return f, nil
}

// Validate reports each floor that is missing or not positive, by its TOML key path.
func (f Floors) Validate() error {
	var errs []error
	for _, v := range []struct {
		key   string
		value float64
	}{
		{"pool.free_mib", f.Pool.FreeMiB},
		{"agent.decode_tok_s", f.Agent.DecodeTokS},
		{"agent.prefill_tok_s", f.Agent.PrefillTokS},
		{"agent.ttft_8k_s", f.Agent.TTFT8kS},
		{"agent.stream4_decode_tok_s", f.Agent.Stream4DecodeTokS},
		{"vision.image_answer_s", f.Vision.ImageAnswerS},
		{"audio.rtfx", f.Audio.RTFx},
		{"audio.stream4_rtfx", f.Audio.Stream4RTFx},
		{"embedding.throughput_tok_s", f.Embedding.ThroughputTokS},
		{"embedding.query32_ms", f.Embedding.Query32MS},
	} {
		if !(v.value > 0) {
			errs = append(errs, fmt.Errorf("%s: required, a positive number", v.key))
		}
	}
	return errors.Join(errs...)
}
