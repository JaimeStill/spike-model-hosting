// Package profile loads a host-class profile from TOML, validates it, and renders the serving
// configuration it describes: today, the llama.cpp router's model presets.
//
// A profile describes a class of host (its memory pool and backend), never one host: it carries
// no host name or address. Values that belong to one machine are given at render time.
package profile

import (
	"errors"
	"fmt"
	"regexp"
	"slices"

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
// field names below; the testdata directory holds a complete example.
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
}

// minBuild matches a llama.cpp build tag.
var minBuild = regexp.MustCompile(`^b[0-9]+$`)

// Load reads the profile at path and validates it. A profile that fails validation returns its
// errors, each naming the field at fault by its TOML key path.
func Load(path string) (*Profile, error) {
	var p Profile
	if _, err := toml.DecodeFile(path, &p); err != nil {
		return nil, fmt.Errorf("profile: %w", err)
	}
	if err := p.Validate(); err != nil {
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
	}
	return errors.Join(errs...)
}
