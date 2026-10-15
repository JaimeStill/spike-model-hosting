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
//     its own MemoryBudget. Exactly one set is the startup set, which Profile.StartupSet
//     returns, and every model is in at least one set.
//   - Each Model declares its roles and its licence beside its origin.
//   - HostStrixHalo and HostNvidiaCUDA, EngineLlamaCPP, BindTailnet and BindLoopback, KVUnified
//     and KVPerSlot, KindGenerative and KindEmbedding, RoleChat, RoleVision, RoleAudio and
//     RoleEmbedding, and ReasoningLow, ReasoningMedium and ReasoningHigh are the values its
//     enumerated fields allow.
//
// The renders:
//   - Profile.RenderPresets writes the router's --models-preset INI file, every model's section
//     with load-on-startup only for the startup set's models.
//   - Profile.RenderUnit writes the router's systemd unit, with a Host's values; Host.Validate
//     reports a Host value that is missing or that the unit can't carry.
//   - EngineBinary, PresetsPath and RouterPort are the install layout every unit renders.
package profile
