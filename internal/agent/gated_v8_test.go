package agent

import "testing"

// TestGatedToolsV8 checks that web_fetch joins the gate list and that the
// gate matches tool names exactly.
func TestGatedToolsV8(t *testing.T) {
	gated := []string{"bash", "write_file", "edit_file", "spawn_agent", "web_search", "web_fetch"}
	for _, name := range gated {
		if !IsGated(name) {
			t.Errorf("IsGated(%q) = false, want true", name)
		}
	}
	notGated := []string{"read_file", "glob", "grep", "question", "wait_agent", "cancel_agent", "Web_Fetch", "web_fetch ", "fetch", ""}
	for _, name := range notGated {
		if IsGated(name) {
			t.Errorf("IsGated(%q) = true, want false", name)
		}
	}
}
