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
// field names below; devices/strix-halo/profile.toml, under the module root, is a complete
// example.
type Profile struct {
	// HostClass names the class of host the profile serves: HostStrixHalo or HostNvidiaCUDA.
	HostClass string `toml:"host_class"`
	Engine    Engine `toml:"engine"`
	// Bind is BindTailnet or BindLoopback.
	Bind string `toml:"bind"`
	// Slots is the number of requests each model serves at once (llama.cpp's parallel).
	Slots int `toml:"slots"`
	// KV is KVUnified or KVPerSlot.
	KV string `toml:"kv"`
	// MemoryBandwidthGBs is the host class's memory bandwidth in GB/s (10^9 bytes a second), such
	// as Strix Halo's 256: the ceiling a decode's speed is compared with, never gated on. It is
	// optional; a profile without it gets no ceiling comparison.
	MemoryBandwidthGBs float64  `toml:"memory_bandwidth_gb_s"`
	Defaults           Defaults `toml:"defaults"`
	// Models are every model the profile serves, in any of its sets.
	Models []Model `toml:"models"`
	// Sets are the named groups of models the router loads together; exactly one is the
	// startup set.
	Sets []Set `toml:"sets"`
}

// MaxSetModels is the most models a set may hold: the router's default --models-max, the most
// it keeps loaded at once. The router refuses to start when more presets load on startup.
const MaxSetModels = 4

// Set is a named group of the profile's models, loaded side by side and sized together against
// the pool. The startup set's models render with load-on-startup, so the router loads them when
// it starts; every other set's models render without it and stay unloaded until they are
// loaded over the router's API.
type Set struct {
	// Name names the set, such as a; it is unique within the profile.
	Name string `toml:"name"`
	// Startup marks the set the router loads when it starts; exactly one set sets it.
	Startup bool `toml:"startup"`
	// Models are the set's models by router ID, each one the profile lists: at most
	// MaxSetModels, at least one of them a chat model.
	Models []string `toml:"models"`
	// Roles names the model of the set's that each role's requests go to: the model the
	// spike's probe and instruments send chat, images, audio and embeddings to.
	Roles SetRoles `toml:"roles"`
	// MemoryBudget is the set's footprint, all its models loaded: measured, or estimated
	// until it is measured.
	MemoryBudget MemoryBudget `toml:"memory_budget"`
}

// SetRoles assigns each role to one of a set's models, by router ID. A model's Roles say what
// it can serve; a set may hold several models that serve a role, and SetRoles picks the one the
// probe and the instruments send it to. Chat and Embedding are required; Vision and Audio are
// optional, empty for a set that serves neither. Each model named is in the set and declares
// the role it fills. One model may fill several roles, such as a candidate main model that
// takes chat and images both.
type SetRoles struct {
	// Chat is the model every consumer's session runs on, and the model client's chat model.
	Chat string `toml:"chat"`
	// Vision is the model a request with an image goes to.
	Vision string `toml:"vision"`
	// Audio is the model a request with audio goes to.
	Audio string `toml:"audio"`
	// Embedding is the model embeddings are requested from.
	Embedding string `toml:"embedding"`
}

// Models returns the set's role models, chat, vision, audio and embedding in that order, each
// once, the empty ones left out: the models a run on the set needs loaded.
func (r SetRoles) Models() []string {
	var ids []string
	for _, id := range []string{r.Chat, r.Vision, r.Audio, r.Embedding} {
		if id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// Model returns the profile's model whose router ID is id, or nil when it lists none: how a
// tool finds what the profile says of a set's role model, such as its chat model's Consumers.
func (p *Profile) Model(id string) *Model {
	for i := range p.Models {
		if p.Models[i].ID == id {
			return &p.Models[i]
		}
	}
	return nil
}

// StartupSet returns the profile's startup set, or nil when no set is marked startup. A valid
// profile has exactly one.
func (p *Profile) StartupSet() *Set {
	for i := range p.Sets {
		if p.Sets[i].Startup {
			return &p.Sets[i]
		}
	}
	return nil
}

// Set returns the set named name, or the startup set when name is empty: the one lookup the
// probe and every instrument share, so a run on no named set runs on the set the router loads
// when it starts. It fails naming the profile's sets when none is named name.
func (p *Profile) Set(name string) (*Set, error) {
	if name == "" {
		if s := p.StartupSet(); s != nil {
			return s, nil
		}
		return nil, errors.New("profile: no startup set")
	}
	names := make([]string, len(p.Sets))
	for i := range p.Sets {
		if p.Sets[i].Name == name {
			return &p.Sets[i], nil
		}
		names[i] = p.Sets[i].Name
	}
	return nil, fmt.Errorf("profile: no set %q; the profile's sets are %q", name, names)
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

// MemoryBudget records the memory footprint of a set's models, all loaded. Its figures are
// recorded, never computed or checked against the pool: the binding test is a measurement on
// the host, and this is where its result is kept. A set not yet measured records its estimate
// instead, which the measurement joins, never replaces, once the set has loaded on the host.
type MemoryBudget struct {
	// PoolMiB is the memory pool the models load into.
	PoolMiB int `toml:"pool_mib"`
	// MeasuredMiB is the pool used with every model in the set loaded and Slots requests in
	// flight on each. It, EstimatedMiB, or both is set.
	MeasuredMiB int `toml:"measured_mib"`
	// EstimatedMiB is the same figure estimated from the models' file sizes and their caches'
	// costs, for a set not yet measured. The estimate's arithmetic is kept in a comment beside
	// it.
	EstimatedMiB int `toml:"estimated_mib"`
	// Build is the engine build the measurement ran on, required with MeasuredMiB. An estimate
	// alone names none: it ran on no build.
	Build string `toml:"build"`
	// Source names where the full measurement, or the figures an estimate starts from, is kept;
	// it is optional.
	Source string `toml:"source"`
}

// The kinds of model. A generative model samples, so it carries Sampling; an embedding model
// doesn't, and carries none.
const (
	KindGenerative = "generative"
	KindEmbedding  = "embedding"
)

// The roles a model may declare: what a client may send it. A model's roles are a subset of
// these, and every set holds at least one RoleChat model.
const (
	RoleChat      = "chat"
	RoleVision    = "vision"
	RoleAudio     = "audio"
	RoleEmbedding = "embedding"
)

// roles are the values a model's Roles may hold.
var roles = []string{RoleChat, RoleVision, RoleAudio, RoleEmbedding}

// The reasoning efforts a model's Sampling may set. Empty, the default, leaves the effort to the
// chat template.
const (
	ReasoningLow    = "low"
	ReasoningMedium = "medium"
	ReasoningHigh   = "high"
)

// piThinkingLevels are the thinking levels Pi's settings take (Pi 1.3.0's docs/settings.md,
// defaultThinkingLevel), the values a model's Consumers.Pi may hold.
var piThinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

// reservedArgs are the presets keys the profile renders from its typed fields, which a model's
// raw Args may not set. hf-repo renders from the model's ID; hf and hfr are its short aliases.
var reservedArgs = []string{
	"jinja", "n-gpu-layers", "metrics", "checkpoint-min-step", "parallel", "kv-unified",
	"load-on-startup", "hf-repo", "hf", "hfr", "c", "temp", "top-p", "top-k", "min-p",
	"chat-template-kwargs", "mtmd-batch-max-tokens",
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
	// model's presets section and is its hf-repo, the Hugging Face repository and quant tag the
	// router fetches its weights by. Its tag must be the one the router lists it by
	// (RouterTag), or the router lists the model under another ID.
	ID string `toml:"id"`
	// Publisher is the organization that trained and released the model.
	Publisher string `toml:"publisher"`
	// Origin is the publisher's country of origin, by its short English name, such as United
	// States or China. Validation requires it and never judges it; evaluate's admission does, by
	// STANDARDS: a model of OriginChina is chosen only when no alternative serves.
	Origin string `toml:"origin"`
	// OriginReason says why a model whose origin STANDARDS restricts (Model.OriginRestricted)
	// was chosen: the alternatives that don't serve. Only such a model sets it, and admission
	// fails one without it.
	OriginReason string `toml:"origin_reason"`
	// Licence is the licence the model's weights are released under, as its card states it,
	// such as Apache-2.0. Like Origin, validation requires it and never judges it.
	Licence string `toml:"licence"`
	// Roles are what the model serves: RoleChat, RoleVision, RoleAudio or RoleEmbedding. A
	// generative model takes any but RoleEmbedding; an embedding model takes RoleEmbedding
	// alone.
	Roles []string `toml:"roles"`
	// CardURL is the model card the model's settings are traced to.
	CardURL string `toml:"card_url"`
	// C is the model's context size, at most CardContext.
	C int `toml:"c"`
	// CardContext is the longest context the model's card states, in tokens, or its config's
	// max_position_embeddings where the card states none: the ceiling C may not pass.
	CardContext int `toml:"card_context"`
	// ActiveParamsB is the parameters, in billions, the model reads to decode one token, as its
	// card states them: the active ones of a mixture of experts, or the effective ones of a model
	// whose per-layer embeddings are a lookup. With BitsPerWeight it estimates the bytes a token
	// reads. Optional; set with BitsPerWeight or not at all.
	ActiveParamsB float64 `toml:"active_params_b"`
	// BitsPerWeight is the served weights' average bits per parameter: the GGUF's size in bits
	// over its parameter count. Optional; set with ActiveParamsB or not at all.
	BitsPerWeight float64 `toml:"bits_per_weight"`
	// CReason says why the model runs at C. It renders as a comment above the model's section.
	CReason string `toml:"c_reason"`
	// MtmdBatchMaxTokens caps the image tokens the model's image encoder takes in one batch
	// (llama-server's --mtmd-batch-max-tokens, 1024 by default), so images that would batch
	// together are encoded one batch at a time: the encoder's compute buffer, which grows to the
	// largest batch it has run and never shrinks, stays bounded. Optional; zero leaves llama.cpp's
	// default. Only a model that declares RoleVision sets it, with its reason.
	MtmdBatchMaxTokens int `toml:"mtmd_batch_max_tokens"`
	// MtmdBatchMaxTokensReason says why the model caps its image batch at MtmdBatchMaxTokens;
	// required with it. It renders as a comment above the model's section.
	MtmdBatchMaxTokensReason string `toml:"mtmd_batch_max_tokens_reason"`
	// Kind is KindGenerative or KindEmbedding.
	Kind string `toml:"kind"`
	// Dims is an embedding model's output width, as its card states it: required for an
	// embedding model and refused for a generative one. The probe checks an embedding's width
	// against it.
	Dims int `toml:"dims"`
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
	// Consumers is the reasoning setting each coding agent the spike drives sends when the
	// model is a set's chat model; the zero value leaves each agent at its own default. It is
	// the consumers' configuration, not the router's, so it renders into no preset.
	Consumers ConsumerReasoning `toml:"consumers"`
}

// ConsumerReasoning is the reasoning setting a coding agent sends with a chat model's
// requests, per agent, chosen so each sends a value the model's chat template accepts. Claude
// Code has none: the spike runs it unchanged. A model that sets any of it is a generative
// model that declares the chat role, and gives the Reason.
type ConsumerReasoning struct {
	// Pi is Pi's thinking level for the model, one of off, minimal, low, medium, high, xhigh
	// and max, written into Pi's per-model settings (modelThinkingLevels). Pi clamps it to the
	// levels it reads the model as supporting, and maps the result to the request its
	// provider sends.
	Pi string `toml:"pi"`
	// OpenCode are the options OpenCode sends with each of the model's requests, set on the
	// model in OpenCode's configuration: an AI SDK option such as reasoningEffort, which it
	// sends as reasoning_effort, or a request field it passes through as given, such as
	// chat_template_kwargs.
	OpenCode map[string]any `toml:"opencode"`
	// Reason says why each agent takes its value: what it sends, and that the model's template
	// accepts it. Required when Pi or OpenCode is set.
	Reason string `toml:"reason"`
}

// set reports whether c sets anything.
func (c ConsumerReasoning) set() bool { return c.Pi != "" || len(c.OpenCode) > 0 || c.Reason != "" }

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

// freeForm is the key path of the one table whose keys the schema leaves to the profile:
// Model.Consumers.OpenCode, OpenCode's options.
var freeForm = []string{"models", "consumers", "opencode"}

// minBuild matches a llama.cpp build tag.
var minBuild = regexp.MustCompile(`^b[0-9]+$`)

// quantTag matches the quant type at the end of a tag, after its last '-' or '.', as llama.cpp
// reads it (common/preset.cpp, canonical_tag, at b11669).
var quantTag = regexp.MustCompile(`(?i)[-.]([A-Z0-9_]+)$`)

// RouterTag returns the tag the llama.cpp router lists a presets section's model by: the quant
// type at the end of tag, upper-cased. The router renames a section whose tag differs, so
// unsloth's UD-Q4_K_XL lists as Q4_K_XL, and Q8_0 as Q8_0. The tag still selects the weights:
// the fetch picks the repository's first model file whose name holds the tag followed by '.' or
// '-' (common/download.cpp, find_best_model), so a canonical tag fetches the same file where
// the repository holds no other file it matches.
func RouterTag(tag string) string {
	if m := quantTag.FindStringSubmatch(tag); m != nil {
		return strings.ToUpper(m[1])
	}
	return strings.ToUpper(tag)
}

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
		// A consumer's options are free-form, passed to the consumer as given; the decoder
		// reports a table nested in them as undecoded though it is kept.
		if slices.Equal(key[:min(len(key), len(freeForm))], freeForm) {
			continue
		}
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
	if p.MemoryBandwidthGBs < 0 {
		errs = append(errs, fmt.Errorf("memory_bandwidth_gb_s: %v is below 0", p.MemoryBandwidthGBs))
	}

	positive("defaults.c", p.Defaults.C)
	positive("defaults.gpu_layers", p.Defaults.GPULayers)
	positive("defaults.checkpoint_min_step", p.Defaults.CheckpointMinStep)

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
		// The router splits a section's name at its last ':' (common/preset.cpp).
		if colon := strings.LastIndex(m.ID, ":"); colon >= 0 {
			if repo, tag := m.ID[:colon+1], m.ID[colon+1:]; tag != RouterTag(tag) {
				errs = append(errs, fmt.Errorf("%s: %q lists on the router as %q; name it so",
					field("id"), m.ID, repo+RouterTag(tag)))
			}
		}
		required(field("publisher"), m.Publisher)
		required(field("origin"), m.Origin)
		if m.OriginReason != "" && !m.OriginRestricted() {
			errs = append(errs, fmt.Errorf("%s: only a model whose origin is %s records why it was chosen, not one of %q",
				field("origin_reason"), OriginChina, m.Origin))
		}
		required(field("licence"), m.Licence)
		errs = append(errs, m.validateRoles(field("roles"))...)
		required(field("card_url"), m.CardURL)
		positive(field("c"), m.C)
		positive(field("card_context"), m.CardContext)
		if m.CardContext > 0 && m.C > m.CardContext {
			errs = append(errs, fmt.Errorf("%s: %d is over the card's context, %d", field("c"), m.C, m.CardContext))
		}
		required(field("c_reason"), m.CReason)
		errs = append(errs, m.validateImageBatch(field("mtmd_batch_max_tokens"))...)
		switch {
		case m.ActiveParamsB < 0 || m.BitsPerWeight < 0:
			errs = append(errs, fmt.Errorf("%s: active_params_b and bits_per_weight are positive", field("active_params_b")))
		case (m.ActiveParamsB == 0) != (m.BitsPerWeight == 0):
			errs = append(errs, fmt.Errorf("%s: set with bits_per_weight, or neither", field("active_params_b")))
		case m.BitsPerWeight > 32:
			errs = append(errs, fmt.Errorf("%s: %v is over 32", field("bits_per_weight"), m.BitsPerWeight))
		}
		oneOf(field("kind"), m.Kind, KindGenerative, KindEmbedding)
		switch {
		case m.Kind == KindGenerative && m.Sampling == nil:
			errs = append(errs, fmt.Errorf("%s: required for a generative model", field("sampling")))
		case m.Kind == KindGenerative:
			errs = append(errs, m.Sampling.validate(field("sampling"))...)
		case m.Kind == KindEmbedding && m.Sampling != nil:
			errs = append(errs, fmt.Errorf("%s: an embedding model doesn't sample", field("sampling")))
		}
		switch {
		case m.Kind == KindEmbedding:
			positive(field("dims"), m.Dims)
		case m.Dims != 0:
			errs = append(errs, fmt.Errorf("%s: only an embedding model has an output width", field("dims")))
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
		errs = append(errs, m.validateConsumers(field("consumers"))...)
	}

	if len(p.Sets) == 0 {
		errs = append(errs, errors.New("sets: required, at least one set"))
	}
	models := make(map[string]Model, len(p.Models))
	for _, m := range p.Models {
		models[m.ID] = m
	}
	names := make(map[string]bool, len(p.Sets))
	inSet := make(map[string]bool, len(p.Models))
	var startup []string
	for i, s := range p.Sets {
		field := func(name string) string { return fmt.Sprintf("sets[%d].%s", i, name) }
		required(field("name"), s.Name)
		if s.Name != "" && names[s.Name] {
			errs = append(errs, fmt.Errorf("%s: %q is listed twice", field("name"), s.Name))
		}
		names[s.Name] = true
		if s.Startup {
			startup = append(startup, s.Name)
		}
		if len(s.Models) > MaxSetModels {
			errs = append(errs, fmt.Errorf("%s: %d models, over the router's %d", field("models"), len(s.Models), MaxSetModels))
		}
		chat := false
		listed := make(map[string]bool, len(s.Models))
		for _, id := range s.Models {
			m, ok := models[id]
			switch {
			case !ok:
				errs = append(errs, fmt.Errorf("%s: %q is not a model the profile lists", field("models"), id))
			case listed[id]:
				errs = append(errs, fmt.Errorf("%s: %q is listed twice", field("models"), id))
			}
			listed[id] = true
			inSet[id] = true
			chat = chat || slices.Contains(m.Roles, RoleChat)
		}
		if !chat {
			errs = append(errs, fmt.Errorf("%s: required, at least one %s model", field("models"), RoleChat))
		}
		errs = append(errs, s.validateRoles(field("roles"), models)...)
		budget, b := field("memory_budget"), s.MemoryBudget
		positive(budget+".pool_mib", b.PoolMiB)
		switch {
		case b.MeasuredMiB == 0 && b.EstimatedMiB == 0:
			errs = append(errs, fmt.Errorf("%s.measured_mib: required, or estimated_mib for a set not yet measured", budget))
		case b.MeasuredMiB < 0:
			positive(budget+".measured_mib", b.MeasuredMiB)
		case b.EstimatedMiB < 0:
			positive(budget+".estimated_mib", b.EstimatedMiB)
		}
		// A measurement names the build it ran on; an estimate alone ran on none, though it may
		// name one.
		if b.MeasuredMiB != 0 || b.EstimatedMiB == 0 || b.Build != "" {
			build(budget+".build", b.Build)
		}
	}
	switch {
	case len(p.Sets) > 0 && len(startup) == 0:
		errs = append(errs, errors.New("sets: required, one set with startup = true"))
	case len(startup) > 1:
		errs = append(errs, fmt.Errorf("sets: %q are all startup sets; want exactly one", startup))
	}
	// A model in no set is refused: the router loads only the startup set, and the profile
	// names no other way a model joins the pool, so a model in no set is configuration nothing
	// loads. A model staged for later is listed with the set it joins.
	for i, m := range p.Models {
		if m.ID != "" && !inSet[m.ID] {
			errs = append(errs, fmt.Errorf("models[%d].id: %q is in no set", i, m.ID))
		}
	}
	return errors.Join(errs...)
}

// validateRoles reports a required role the set leaves unassigned, and each role assigned to a
// model the set doesn't hold or that doesn't declare the role, under the key path prefix.
// models are the profile's models by router ID.
func (s Set) validateRoles(prefix string, models map[string]Model) []error {
	var errs []error
	for _, r := range []struct {
		role, id string
		required bool
	}{
		{RoleChat, s.Roles.Chat, true},
		{RoleVision, s.Roles.Vision, false},
		{RoleAudio, s.Roles.Audio, false},
		{RoleEmbedding, s.Roles.Embedding, true},
	} {
		field := prefix + "." + r.role
		switch {
		case r.id == "" && r.required:
			errs = append(errs, fmt.Errorf("%s: required", field))
		case r.id == "":
		case !slices.Contains(s.Models, r.id):
			errs = append(errs, fmt.Errorf("%s: %q is not one of the set's models", field, r.id))
		case !slices.Contains(models[r.id].Roles, r.role):
			errs = append(errs, fmt.Errorf("%s: %q doesn't declare the %s role", field, r.id, r.role))
		}
	}
	return errs
}

// validateRoles reports each of the model's roles outside the closed set, a role listed twice,
// no role at all, and a role its kind can't take, under the key path prefix.
func (m Model) validateRoles(prefix string) []error {
	if len(m.Roles) == 0 {
		return []error{fmt.Errorf("%s: required, at least one of %q", prefix, roles)}
	}
	var errs []error
	for _, r := range m.Roles {
		if !slices.Contains(roles, r) {
			errs = append(errs, fmt.Errorf("%s: %q is not one of %q", prefix, r, roles))
		}
	}
	if len(m.Roles) != len(slices.Compact(slices.Sorted(slices.Values(m.Roles)))) {
		errs = append(errs, fmt.Errorf("%s: %q lists a role twice", prefix, m.Roles))
	}
	embedding := slices.Contains(m.Roles, RoleEmbedding)
	switch {
	case m.Kind == KindEmbedding && (!embedding || len(m.Roles) > 1):
		errs = append(errs, fmt.Errorf("%s: an embedding model takes the %s role alone", prefix, RoleEmbedding))
	case m.Kind == KindGenerative && embedding:
		errs = append(errs, fmt.Errorf("%s: a generative model can't take the %s role", prefix, RoleEmbedding))
	}
	return errs
}

// OriginChina is the origin STANDARDS restricts: a model of Chinese origin is chosen only when no
// alternative serves, and its entry records why (OriginReason).
const OriginChina = "China"

// OriginRestricted reports whether the model's origin is one STANDARDS restricts, matched
// exactly as the profile writes origins: OriginChina.
func (m Model) OriginRestricted() bool { return m.Origin == OriginChina }

// validateImageBatch reports an image batch cap that isn't positive, one on a model that
// declares no vision role, and a cap or a reason without the other, under the key path field.
// A model that sets neither has nothing to report.
func (m Model) validateImageBatch(field string) []error {
	if m.MtmdBatchMaxTokens == 0 && m.MtmdBatchMaxTokensReason == "" {
		return nil
	}
	var errs []error
	if m.MtmdBatchMaxTokens <= 0 {
		errs = append(errs, fmt.Errorf("%s: required with its reason, a positive integer", field))
	}
	if !slices.Contains(m.Roles, RoleVision) {
		errs = append(errs, fmt.Errorf("%s: only a model with the %s role encodes images", field, RoleVision))
	}
	if m.MtmdBatchMaxTokensReason == "" {
		errs = append(errs, fmt.Errorf("%s_reason: required", field))
	}
	return errs
}

// validateConsumers reports a consumer reasoning setting on a model that can't be a chat model,
// a Pi thinking level Pi doesn't take, and a setting without its reason, under the key path
// prefix.
func (m Model) validateConsumers(prefix string) []error {
	c := m.Consumers
	if !c.set() {
		return nil
	}
	var errs []error
	if m.Kind != KindGenerative || !slices.Contains(m.Roles, RoleChat) {
		errs = append(errs, fmt.Errorf("%s: only a generative %s model runs under a consumer", prefix, RoleChat))
	}
	if c.Pi == "" && len(c.OpenCode) == 0 {
		errs = append(errs, fmt.Errorf("%s: sets neither pi nor opencode", prefix))
	}
	if c.Pi != "" && !slices.Contains(piThinkingLevels, c.Pi) {
		errs = append(errs, fmt.Errorf("%s.pi: %q is not one of Pi's thinking levels %q", prefix, c.Pi, piThinkingLevels))
	}
	if c.Reason == "" {
		errs = append(errs, fmt.Errorf("%s.reason: required", prefix))
	}
	return errs
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
