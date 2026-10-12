package profile

import (
	"errors"
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// The host classes a profile may name. A CUDA profile validates; nothing renders it yet.
const (
	HostStrixHalo  = "strix-halo"
	HostNvidiaCUDA = "nvidia-cuda"
)

// EngineLlamaCPP is the one engine a profile names today.
const EngineLlamaCPP = "llama.cpp"

// The bind modes. Tailnet binds the router to the tailnet IPv4 the host resolves when the unit
// starts, so no address is ever written into a profile; loopback binds it to 127.0.0.1.
const (
	BindTailnet  = "tailnet"
	BindLoopback = "loopback"
)

// The KV cache modes. Unified shares one KV pool, sized to a model's c, across its slots;
// per-slot gives each slot its own.
const (
	KVUnified = "unified"
	KVPerSlot = "per-slot"
)

// Profile is a host-class profile, decoded from TOML. Its keys are snake_case versions of the
// field names below; profiles/strix-halo.toml, at the repository root, is a complete example.
type Profile struct {
	// HostClass names the class of host the profile serves: HostStrixHalo or HostNvidiaCUDA.
	HostClass string `toml:"host_class"`
	Engine    Engine `toml:"engine"`
	// Bind is BindTailnet or BindLoopback.
	Bind string `toml:"bind"`
	// Slots is the number of requests each model serves at once (llama.cpp's parallel).
	Slots int `toml:"slots"`
	// KV is KVUnified or KVPerSlot.
	KV           string       `toml:"kv"`
	Defaults     Defaults     `toml:"defaults"`
	MemoryBudget MemoryBudget `toml:"memory_budget"`
	Models       []Model      `toml:"models"`
}

// Engine is the serving engine and the oldest build the profile runs on. MinBuild is a floor,
// not a pin: a host may run any build at or above it, and each measurement names its own.
type Engine struct {
	// Name is the engine, EngineLlamaCPP.
	Name string `toml:"name"`
	// Backend is the engine's compute backend, such as vulkan, rocm or cuda.
	Backend string `toml:"backend"`
	// MinBuild is a llama.cpp b-number, such as b11452.
	MinBuild string `toml:"min_build"`
}

// engineRepositories maps each engine a profile may name to its upstream repository.
var engineRepositories = map[string]string{EngineLlamaCPP: "ggml-org/llama.cpp"}

// Repository returns the engine's upstream repository as owner/name on GitHub, such as
// ggml-org/llama.cpp, or "" for an engine the schema doesn't name.
func (e Engine) Repository() string {
	return engineRepositories[e.Name]
}

// Defaults are the settings shared by every model, rendered into the presets' [*] section.
// A model without its own section, pulled ad hoc, runs at these.
type Defaults struct {
	// C is the context size of a model without its own section.
	C int `toml:"c"`
	// GPULayers is how many layers to offload (llama.cpp's n-gpu-layers); 999 means all.
	GPULayers int `toml:"gpu_layers"`
	// CheckpointMinStep is the spacing, in tokens, between prompt-cache checkpoints.
	CheckpointMinStep int `toml:"checkpoint_min_step"`
}

// MemoryBudget records the measured memory footprint of the profile's models, all loaded. Its
// figures are recorded, never computed or checked against the pool: the binding test is a
// measurement on the host, and this is where its result is kept.
type MemoryBudget struct {
	// PoolMiB is the memory pool the models load into.
	PoolMiB int `toml:"pool_mib"`
	// MeasuredMiB is the pool used with every model loaded and Slots requests in flight on each.
	MeasuredMiB int `toml:"measured_mib"`
	// Build is the engine build the measurement ran on.
	Build string `toml:"build"`
	// Source names where the full measurement is kept; it is optional.
	Source string `toml:"source"`
}

// The kinds of model. A generative model samples, so it carries Sampling; an embedding model
// doesn't, and carries none.
const (
	KindGenerative = "generative"
	KindEmbedding  = "embedding"
)

// The reasoning efforts a model's Sampling may set. Empty, the default, leaves the effort to the
// chat template.
const (
	ReasoningLow    = "low"
	ReasoningMedium = "medium"
	ReasoningHigh   = "high"
)

// reservedArgs are the presets keys the profile renders from its typed fields, which a model's
// raw Args may not set.
var reservedArgs = []string{
	"jinja", "n-gpu-layers", "metrics", "checkpoint-min-step", "parallel", "kv-unified",
	"load-on-startup", "c", "temp", "top-p", "top-k", "min-p", "chat-template-kwargs",
}

// hostArgs are the keys the unit or the host owns, which a model's raw Args may not set either:
// the router's own flags, which the rendered unit sets on its command line (unit.service.tmpl),
// and the llama-server flags that take a host path, which a host-free profile can't name. Short
// aliases are listed beside their long names. It is a fixed list of keys; values aren't scanned.
var hostArgs = []string{
	// Set by the rendered unit, or router-wide.
	"host", "port", "reuse-port", "models-dir", "models-preset", "models-max", "models-autoload",
	"no-models-autoload",
	// Take a host path.
	"m", "model", "mm", "mmproj", "md", "model-draft", "spec-draft-model", "lora", "lora-scaled",
	"control-vector", "control-vector-scaled", "grammar-file", "jf", "json-schema-file", "lcs",
	"lookup-cache-static", "lcd", "lookup-cache-dynamic", "chat-template-file", "log-file",
	"log-prompts-dir", "slot-save-path", "media-path", "path", "api-key-file", "ssl-key-file",
	"ssl-cert-file", "ui-config-file", "webui-config-file", "mcp-servers-config",
	"video-ffmpeg-dir",
}

// Model is one model the router serves, with the provenance its settings come from.
type Model struct {
	// ID is the model's router ID, such as ggml-org/gpt-oss-120b-GGUF:MXFP4; it names the
	// model's presets section.
	ID string `toml:"id"`
	// Publisher is the organization that trained and released the model.
	Publisher string `toml:"publisher"`
	// Origin is the publisher's country of origin. Validation records it and never judges it.
	Origin string `toml:"origin"`
	// CardURL is the model card the model's settings are traced to.
	CardURL string `toml:"card_url"`
	// C is the model's context size.
	C int `toml:"c"`
	// CReason says why the model runs at C. It renders as a comment above the model's section.
	CReason string `toml:"c_reason"`
	// Kind is KindGenerative or KindEmbedding.
	Kind string `toml:"kind"`
	// Sampling is a generative model's sampling, required for one and refused for an embedding
	// model.
	Sampling *Sampling `toml:"sampling"`
	// Args are llama.cpp keys the schema doesn't type, such as pooling, b, ub, no-mmproj and
	// embeddings, keyed by llama-server argument name without its dashes. They render into the
	// model's section as given, in key order. A value is a string, integer, float or boolean.
	Args map[string]any `toml:"args"`
	// ArgsReason says why the model sets Args; required when it sets any. It renders as a comment
	// above the model's section.
	ArgsReason string `toml:"args_reason"`
}

// Sampling is a generative model's sampling, every value traced to Source. Each value is
// required, so the card, not the engine's or the conversion's defaults, is what the model runs
// at; a truncation the card doesn't name is turned off (top-k 0, min-p 0).
type Sampling struct {
	// Source is the page the values come from: the model card, or the README it defers to.
	Source string `toml:"source"`
	// Temperature is at least 0.
	Temperature *float64 `toml:"temperature"`
	// TopP is in [0, 1].
	TopP *float64 `toml:"top_p"`
	// TopK is at least 0; 0 turns the truncation off.
	TopK *int `toml:"top_k"`
	// MinP is in [0, 1]; 0 turns the truncation off.
	MinP *float64 `toml:"min_p"`
	// Reasoning is ReasoningLow, ReasoningMedium or ReasoningHigh, or empty for the chat
	// template's default. Set, it renders as the template's reasoning_effort.
	Reasoning string `toml:"reasoning"`
	// Reason says why the model samples at these values. It renders as a comment above the
	// model's section.
	Reason string `toml:"reason"`
}

// Host is the render-time input naming what belongs to one machine, never to a profile: the
// service user the router runs as, its HF_HOME, and the directory its models are kept in.
type Host struct {
	ServiceUser string
	HFHome      string
	ModelsDir   string
}

// Validate reports each Host input that is empty, by name. A render that uses the host's values
// calls it first, so it fails rather than render a unit with a blank in it. It also refuses a
// value the unit can't carry as given: one with whitespace, a quote or backtick, a backslash, a
// dollar sign or a percent sign, which the unit's shell or systemd would split or expand; and a
// relative HF_HOME or models directory.
func (h Host) Validate() error {
	var errs []error
	for _, in := range []struct {
		name, value string
		path        bool
	}{
		{"service user", h.ServiceUser, false},
		{"HF_HOME", h.HFHome, true},
		{"models directory", h.ModelsDir, true},
	} {
		switch {
		case in.value == "":
			errs = append(errs, fmt.Errorf("host %s: required", in.name))
		case strings.ContainsAny(in.value, unsafeHostChars):
			errs = append(errs, fmt.Errorf("host %s: %q has a character the unit can't carry as given", in.name, in.value))
		case in.path && !path.IsAbs(in.value):
			errs = append(errs, fmt.Errorf("host %s: %q is not an absolute path", in.name, in.value))
		}
	}
	return errors.Join(errs...)
}

// unsafeHostChars are the characters a Host value may not hold: whitespace and the characters
// the unit's shell wrapper or systemd would read as syntax.
const unsafeHostChars = " \t\n\r\v\f'\"\\$%`"

// minBuild matches a llama.cpp build tag.
var minBuild = regexp.MustCompile(`^b[0-9]+$`)

// Load reads the profile at path and validates it. A profile that fails validation returns its
// errors, each naming the field at fault by its TOML key path; a key the schema doesn't define
// is one of them.
func Load(path string) (*Profile, error) {
	var p Profile
	md, err := toml.DecodeFile(path, &p)
	if err != nil {
		return nil, fmt.Errorf("profile: %w", err)
	}
	var errs []error
	for _, key := range md.Undecoded() {
		errs = append(errs, fmt.Errorf("%s: not a key the schema defines", key))
	}
	if err := errors.Join(append(errs, p.Validate())...); err != nil {
		return nil, fmt.Errorf("profile %s: %w", path, err)
	}
	return &p, nil
}

// Validate reports every required field the profile is missing and every value outside the
// set its field allows, joined into one error. Each error names its field by TOML key path.
func (p *Profile) Validate() error {
	var errs []error
	required := func(field, value string) {
		if value == "" {
			errs = append(errs, fmt.Errorf("%s: required", field))
		}
	}
	positive := func(field string, value int) {
		if value <= 0 {
			errs = append(errs, fmt.Errorf("%s: required, a positive integer", field))
		}
	}
	oneOf := func(field, value string, allowed ...string) {
		if value == "" {
			required(field, value)
			return
		}
		if slices.Contains(allowed, value) {
			return
		}
		errs = append(errs, fmt.Errorf("%s: %q is not one of %q", field, value, allowed))
	}
	build := func(field, value string) {
		if value == "" {
			required(field, value)
		} else if !minBuild.MatchString(value) {
			errs = append(errs, fmt.Errorf("%s: %q is not a llama.cpp build such as b11452", field, value))
		}
	}

	oneOf("host_class", p.HostClass, HostStrixHalo, HostNvidiaCUDA)
	oneOf("engine.name", p.Engine.Name, EngineLlamaCPP)
	required("engine.backend", p.Engine.Backend)
	build("engine.min_build", p.Engine.MinBuild)
	oneOf("bind", p.Bind, BindTailnet, BindLoopback)
	positive("slots", p.Slots)
	oneOf("kv", p.KV, KVUnified, KVPerSlot)

	positive("defaults.c", p.Defaults.C)
	positive("defaults.gpu_layers", p.Defaults.GPULayers)
	positive("defaults.checkpoint_min_step", p.Defaults.CheckpointMinStep)

	positive("memory_budget.pool_mib", p.MemoryBudget.PoolMiB)
	positive("memory_budget.measured_mib", p.MemoryBudget.MeasuredMiB)
	build("memory_budget.build", p.MemoryBudget.Build)

	if len(p.Models) == 0 {
		errs = append(errs, errors.New("models: required, at least one model"))
	}
	seen := make(map[string]bool, len(p.Models))
	for i, m := range p.Models {
		field := func(name string) string { return fmt.Sprintf("models[%d].%s", i, name) }
		required(field("id"), m.ID)
		if m.ID != "" && seen[m.ID] {
			errs = append(errs, fmt.Errorf("%s: %q is listed twice", field("id"), m.ID))
		}
		seen[m.ID] = true
		required(field("publisher"), m.Publisher)
		required(field("origin"), m.Origin)
		required(field("card_url"), m.CardURL)
		positive(field("c"), m.C)
		required(field("c_reason"), m.CReason)
		oneOf(field("kind"), m.Kind, KindGenerative, KindEmbedding)
		switch {
		case m.Kind == KindGenerative && m.Sampling == nil:
			errs = append(errs, fmt.Errorf("%s: required for a generative model", field("sampling")))
		case m.Kind == KindGenerative:
			errs = append(errs, m.Sampling.validate(field("sampling"))...)
		case m.Kind == KindEmbedding && m.Sampling != nil:
			errs = append(errs, fmt.Errorf("%s: an embedding model doesn't sample", field("sampling")))
		}
		for _, key := range slices.Sorted(maps.Keys(m.Args)) {
			name := field("args." + key)
			if slices.Contains(reservedArgs, key) {
				errs = append(errs, fmt.Errorf("%s: rendered from the profile's typed fields", name))
			}
			if slices.Contains(hostArgs, key) {
				errs = append(errs, fmt.Errorf("%s: owned by the unit or the host, not the profile", name))
			}
			switch m.Args[key].(type) {
			case string, int64, float64, bool:
			default:
				errs = append(errs, fmt.Errorf("%s: a %T, not a string, integer, float or boolean", name, m.Args[key]))
			}
		}
		if len(m.Args) > 0 {
			required(field("args_reason"), m.ArgsReason)
		}
	}
	return errors.Join(errs...)
}

// validate reports each Sampling value that is missing or out of range, under the key path
// prefix.
func (s *Sampling) validate(prefix string) []error {
	var errs []error
	report := func(name, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s.%s: "+format, append([]any{prefix, name}, args...)...))
	}
	unit := func(name string, v *float64) {
		if v == nil {
			report(name, "required")
		} else if *v < 0 || *v > 1 {
			report(name, "%v is outside [0, 1]", *v)
		}
	}
	if s.Source == "" {
		report("source", "required")
	}
	if s.Reason == "" {
		report("reason", "required")
	}
	if s.Temperature == nil {
		report("temperature", "required")
	} else if *s.Temperature < 0 {
		report("temperature", "%v is below 0", *s.Temperature)
	}
	unit("top_p", s.TopP)
	if s.TopK == nil {
		report("top_k", "required")
	} else if *s.TopK < 0 {
		report("top_k", "%d is below 0", *s.TopK)
	}
	unit("min_p", s.MinP)
	reasoning := []string{ReasoningLow, ReasoningMedium, ReasoningHigh}
	if s.Reasoning != "" && !slices.Contains(reasoning, s.Reasoning) {
		report("reasoning", "%q is not one of %q, or empty for the template's default", s.Reasoning, reasoning)
	}
	return errs
}
