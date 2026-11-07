package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	globalHeading  = "\n\n# Global AGENTS.md\n"
	projectHeading = "\n\n# AGENTS.md\n"
)

// globalSession builds a session sharing base's config and start directory,
// after setup has prepared the global directory.
func globalSession(t *testing.T, setup func(gdir string)) (*Session, *Session) {
	t.Helper()
	base, dir := newLoopSession(t, nil)
	if base.cfg.GlobalDir == "" {
		t.Fatal("GlobalDir empty")
	}
	if setup != nil {
		setup(base.cfg.GlobalDir)
	}
	s, err := NewSession(Options{Config: base.cfg, StartDir: dir})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(func() {
		s.Cancel()
		waitTurn(t, s, 5*time.Second)
	})
	return base, s
}

func writeGlobal(t *testing.T, gdir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(gdir, AgentsFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGlobalAgentsOrder(t *testing.T) {
	t.Run("both", func(t *testing.T) {
		_, s := globalSession(t, func(gdir string) { writeGlobal(t, gdir, "GLOBAL-NOTE\n") })
		if err := os.WriteFile(filepath.Join(s.StartDir(), AgentsFile), []byte("PROJECT-NOTE\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		s.Reset()
		p := s.SystemPrompt()
		gi, pi := strings.Index(p, globalHeading), strings.Index(p, projectHeading)
		if gi < 0 || pi < 0 || gi >= pi {
			t.Fatalf("order wrong: global %d project %d\n%s", gi, pi, p)
		}
		gn, pn := strings.Index(p, "GLOBAL-NOTE"), strings.Index(p, "PROJECT-NOTE")
		if !(gi < gn && gn < pi && pi < pn) {
			t.Fatalf("notes misplaced: %d %d %d %d", gi, gn, pi, pn)
		}
	})
	t.Run("global-only", func(t *testing.T) {
		_, s := globalSession(t, func(gdir string) { writeGlobal(t, gdir, "GLOBAL-NOTE\n") })
		p := s.SystemPrompt()
		if !strings.HasSuffix(p, globalHeading+"GLOBAL-NOTE\n") {
			t.Fatalf("global section not at end:\n%s", p)
		}
		if strings.Contains(p, projectHeading) {
			t.Fatal("unexpected project section")
		}
	})
	t.Run("reset re-reads", func(t *testing.T) {
		base, s := globalSession(t, func(gdir string) { writeGlobal(t, gdir, "GLOBAL-OLD\n") })
		if !strings.Contains(s.SystemPrompt(), "GLOBAL-OLD") {
			t.Fatal("old note missing")
		}
		writeGlobal(t, base.cfg.GlobalDir, "GLOBAL-NEW\n")
		s.Reset()
		p := s.SystemPrompt()
		if !strings.Contains(p, "GLOBAL-NEW") || strings.Contains(p, "GLOBAL-OLD") {
			t.Fatalf("reset did not re-read:\n%s", p)
		}
	})
}

func TestGlobalAgentsAbsent(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, gdir string)
	}{
		{"missing", func(t *testing.T, gdir string) {}},
		{"empty", func(t *testing.T, gdir string) { writeGlobal(t, gdir, "") }},
		{"whitespace", func(t *testing.T, gdir string) { writeGlobal(t, gdir, " \n\t\r\n  ") }},
		{"directory", func(t *testing.T, gdir string) {
			if err := os.MkdirAll(filepath.Join(gdir, AgentsFile, "x"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			base, dir := newLoopSession(t, nil)
			if err := os.WriteFile(filepath.Join(dir, AgentsFile), []byte("PROJECT-NOTE\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			base.Reset()
			want := base.SystemPrompt()
			c.setup(t, base.cfg.GlobalDir)
			s, err := NewSession(Options{Config: base.cfg, StartDir: dir})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { s.Cancel(); waitTurn(t, s, 5*time.Second) })
			if got := s.SystemPrompt(); got != want {
				t.Fatalf("prompt changed:\n%q\nwant\n%q", got, want)
			}
		})
	}
	t.Run("no global dir", func(t *testing.T) {
		base, dir := newLoopSession(t, nil)
		want := base.SystemPrompt()
		writeGlobal(t, base.cfg.GlobalDir, "GLOBAL-NOTE\n")
		cfg := *base.cfg
		cfg.GlobalDir = ""
		s, err := NewSession(Options{Config: &cfg, StartDir: dir})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Cancel(); waitTurn(t, s, 5*time.Second) })
		if got := s.SystemPrompt(); got != want {
			t.Fatalf("prompt changed:\n%q\nwant\n%q", got, want)
		}
	})
}

func TestGlobalAgentsTruncate(t *testing.T) {
	t.Run("long", func(t *testing.T) {
		_, s := globalSession(t, func(gdir string) {
			writeGlobal(t, gdir, strings.Repeat("x", 40000)+"TAILMARK")
		})
		p := s.SystemPrompt()
		i := strings.Index(p, globalHeading)
		if i < 0 {
			t.Fatal("no global section")
		}
		section := p[i+len(globalHeading):]
		if section != strings.Repeat("x", 30000) {
			t.Fatalf("section has %d chars, want 30000 x", len(section))
		}
	})
	t.Run("invalid utf8", func(t *testing.T) {
		_, s := globalSession(t, func(gdir string) { writeGlobal(t, gdir, "a\xff\xfeb\n") })
		if !strings.HasSuffix(s.SystemPrompt(), globalHeading+"a�b\n") {
			t.Fatalf("invalid bytes not replaced: %q", s.SystemPrompt())
		}
	})
}

func TestGlobalAgentsRedact(t *testing.T) {
	_, s := globalSession(t, func(gdir string) { writeGlobal(t, gdir, "before SEKRET-XYZ after\n") })
	// Redactor is shared with the base session; add the secret then re-read.
	s.cfg.Redactor.Add("SEKRET-XYZ")
	s.Reset()
	p := s.SystemPrompt()
	if strings.Contains(p, "SEKRET-XYZ") {
		t.Fatal("raw secret in prompt")
	}
	if !strings.Contains(p, "before [redacted] after") {
		t.Fatalf("redacted value missing:\n%s", p)
	}
}

func TestGlobalAgentsSymlink(t *testing.T) {
	t.Run("regular target", func(t *testing.T) {
		_, s := globalSession(t, func(gdir string) {
			target := filepath.Join(t.TempDir(), "real.md")
			if err := os.WriteFile(target, []byte("GLOBAL-LINKED\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(gdir, AgentsFile)); err != nil {
				t.Fatal(err)
			}
		})
		if !strings.HasSuffix(s.SystemPrompt(), globalHeading+"GLOBAL-LINKED\n") {
			t.Fatalf("symlink not followed:\n%s", s.SystemPrompt())
		}
	})
	t.Run("dangling", func(t *testing.T) {
		base, s := globalSession(t, func(gdir string) {
			if err := os.Symlink(filepath.Join(gdir, "nope"), filepath.Join(gdir, AgentsFile)); err != nil {
				t.Fatal(err)
			}
		})
		if s.SystemPrompt() != base.SystemPrompt() {
			t.Fatal("dangling symlink changed the prompt")
		}
	})
}
