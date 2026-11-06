package agent

import (
	"strings"
	"testing"

	"yipyip/internal/provider/providertest"
)

const exitCleanupLine = "Processes still running when yip exits (including background jobs and servers) are terminated, so do not expect them to survive the session."

func TestPromptExitCleanup(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	s, _ := newLoopSession(t, srv)
	p := s.SystemPrompt()
	if n := strings.Count(p, exitCleanupLine); n != 1 {
		t.Fatalf("exit cleanup sentence count = %d, want 1:\n%s", n, p)
	}
}
