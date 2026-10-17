// Package profile loads a host-class profile from TOML, validates it, and renders the serving
// configuration it describes: the llama.cpp router's model presets and its systemd unit.
//
// A profile describes a class of host (its memory pool and backend), never one host. Neither a
// profile nor its render carries a host's name or address: the rendered unit resolves the host's
// tailnet address when it starts. Values that belong to one machine (Host) are given at render
// time.
//
// The schema:
//   - Profile is a decoded profile; Load reads and validates one, and Profile.Validate reports
//     what a profile built in code is missing.
//   - Engine is the serving engine and its minimum build; Engine.Repository names its upstream.
//   - Defaults, Model, Sampling, Set and MemoryBudget are the profile's tables.
//   - A Set names up to MaxSetModels of the profile's models, at least one a chat model, with
//     its own MemoryBudget: measured on a build, or estimated for a set not yet measured.
//     Exactly one set is the startup set, which Profile.StartupSet returns, and every model is
//     in at least one set.
//   - A Set's SetRoles assign each role's requests to one of the set's models that declares the
//     role. Chat and embedding are required, and vision and audio optional; SetRoles.Models lists
//     the assigned models once each. Profile.Set looks a set up by name, or returns the startup
//     set for an empty name; the probe and every instrument share this one lookup.
//   - Each Model declares its roles and its licence beside its origin, its card's context
//     (CardContext, which C may not pass), optionally its active parameters and bits per weight
//     (ActiveParamsB, BitsPerWeight: the bytes a decoded token reads), and an embedding model
//     its output width, Dims; Profile.Model looks one up by router ID. A vision Model may cap
//     the image tokens its encoder takes in one batch (MtmdBatchMaxTokens, with its reason). A
//     profile's optional MemoryBandwidthGBs is its host class's memory bandwidth, the ceiling
//     decode is compared with. RouterTag is the tag the router lists a model by, the one a
//     Model's ID must end with.
//   - A chat Model's ConsumerReasoning is the reasoning setting the coding agents send with its
//     requests, Pi's thinking level and OpenCode's options, each one its template accepts. It
//     configures the consumers, not the router, and renders into no preset.
//   - HostStrixHalo and HostNvidiaCUDA, EngineLlamaCPP, BindTailnet and BindLoopback, KVUnified
//     and KVPerSlot, KindGenerative and KindEmbedding, RoleChat, RoleVision, RoleAudio and
//     RoleEmbedding, and ReasoningLow, ReasoningMedium and ReasoningHigh are the values its
//     enumerated fields allow.
//
// The layout:
//   - Each host class's profile is DevicesDir/<host class>/FileName under the module root, which
//     ModuleRoot finds at or above a directory by its go.mod. Resolve turns a profile argument,
//     a path or a host-class name, into a path, so a command finds a profile by name from any
//     directory of the repository; Open resolves and loads one. Paths lists a devices
//     directory's profiles, and Name gives a profile's host-class name from its path.
//
// The renders:
//   - Profile.RenderPresets writes the router's --models-preset INI file, every model's section
//     with hf-repo = its ID, and load-on-startup only for the startup set's models.
//   - Profile.RenderUnit writes the router's systemd unit, with a Host's values; Host.Validate
//     reports a Host value that is missing or that the unit can't carry.
//   - EngineBinary, PresetsPath and RouterPort are the install layout every unit renders.
package profile
