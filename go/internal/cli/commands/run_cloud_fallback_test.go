package commands

import "testing"

// The MCP run tool (internal/cli/mcp) sets WENDY_RUN_NO_CLOUD_FALLBACK=1 for
// the `wendy run` it spawns so a failed direct connect cannot become a deploy
// to a same-named cloud device.
func TestCloudFallbackDisabledOnlyByMCPRunEnvironment(t *testing.T) {
	for value, want := range map[string]bool{"": false, "0": false, "true": false, "1": true} {
		t.Setenv("WENDY_RUN_NO_CLOUD_FALLBACK", value)
		if got := cloudFallbackDisabled(); got != want {
			t.Errorf("WENDY_RUN_NO_CLOUD_FALLBACK=%q: disabled = %v, want %v", value, got, want)
		}
	}
}
