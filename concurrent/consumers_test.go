package concurrent_test

import (
	"slices"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/concurrent"
)

// TestClaudeCode shows Claude Code's driver runs the given executable from the run's own
// configuration directory, sends every model request to the router as the one model, whichever
// alias asks, and carries nothing else: no API key or token, which would take the place of the
// subscription login.
func TestClaudeCode(t *testing.T) {
	const router, model = "http://router:8080", "ggml-org/gpt-oss-120b-GGUF:MXFP4"
	d := concurrent.ClaudeCode("/bin/claude", "/repo/.state/claude", router, model)
	if d.Command != "/bin/claude" || d.ConfigDir != "/repo/.state/claude" {
		t.Errorf("Command %q, ConfigDir %q; want the executable and the configuration directory given", d.Command, d.ConfigDir)
	}
	want := []string{
		"ANTHROPIC_BASE_URL=" + router,
		"ANTHROPIC_MODEL=" + model,
		"ANTHROPIC_DEFAULT_OPUS_MODEL=" + model,
		"ANTHROPIC_DEFAULT_SONNET_MODEL=" + model,
		"ANTHROPIC_DEFAULT_HAIKU_MODEL=" + model,
	}
	if !slices.Equal(d.Env, want) {
		t.Errorf("Env = %q, want %q", d.Env, want)
	}
}
