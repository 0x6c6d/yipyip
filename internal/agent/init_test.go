package agent

import (
	"strings"
	"testing"
)

func TestInitPromptNew(t *testing.T) {
	p := InitPrompt("")
	for _, want := range []string{"AGENTS.md", "build", "test", "lint", "glob", "grep", "read_file", "write_file"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt does not mention %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, initBeginMarker) {
		t.Errorf("prompt without existing file has the existing-content marker:\n%s", p)
	}
}

func TestInitPromptKeepsExisting(t *testing.T) {
	const existing = "# Notes\nKEEP-ME-LINE\n"
	p := InitPrompt(existing)
	if !strings.Contains(p, initBeginMarker+"\n"+existing+initEndMarker) {
		t.Errorf("existing text not embedded verbatim between markers:\n%s", p)
	}
	for _, want := range []string{"Update it", "keep everything that is still valid"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
	if !strings.Contains(p, "instead of discarding") {
		t.Errorf("prompt does not say not to discard:\n%s", p)
	}
}
