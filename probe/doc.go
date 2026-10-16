// Package probe holds the HTTP probes of a running llama.cpp router, served from a profile's
// rendered presets and unit. The probes are tests behind the integration build tag, run by
// `mise run integration` against the router SPIKE_ROUTER_URL names; the hermetic check builds
// and lints them but never runs them. Every expectation comes from the profile, not the probe:
// the set SPIKE_SET names, or else the startup set, and the model it assigns each role.
package probe
