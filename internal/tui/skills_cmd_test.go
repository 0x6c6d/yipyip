package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/provider/providertest"
)

// typeRunes types s into the input as one key message.
func typeRunes(m Model, s string) Model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	return next.(Model)
}

func TestSkillsCommand(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "ok"})
	defer srv.Close()
	m, sess := skillsModel(t, srv, map[string]string{
		"greet":  "---\ndescription: Say hi\n---\nHello.\n",
		"review": "---\ndescription: PROJECTDESC\n---\nP.\n",
		"help":   "---\ndescription: SHADOWDESC\n---\nS.\n",
		"leak":   "---\ndescription: key SECRET123\n---\nL.\n",
	}, map[string]string{
		"lint":   "---\ndescription: Lint all\n---\nLint.\n",
		"review": "---\ndescription: GLOBALDESC\n---\nG.\n",
	}, nil)
	m, _ = m.Command("/skills")
	v := ansi.Strip(m.View())
	for _, want := range []string{"/greet  Say hi  (project)", "/lint  Lint all  (global)",
		"/review  PROJECTDESC  (project)", "/help  SHADOWDESC  (project)  shadowed by built-in", "key [redacted]"} {
		if !strings.Contains(v, want) {
			t.Errorf("/skills lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "GLOBALDESC") || strings.Contains(v, "SECRET123") {
		t.Errorf("overridden global skill or secret shown:\n%s", v)
	}

	writeSkill(t, filepath.Join(sess.StartDir(), ".yipyip", "skills"), "added", "---\ndescription: ADDEDDESC\n---\nA.\n")
	m, _ = m.Command("/skills")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "/added  ADDEDDESC  (project)") {
		t.Errorf("/skills did not re-read:\n%s", v)
	}
}

func TestSkillsCommandEmpty(t *testing.T) {
	m, _ := skillsModel(t, nil, nil, nil, nil)
	m, _ = m.Command("/skills")
	if v := ansi.Strip(m.View()); !strings.Contains(v, "no skills found") {
		t.Fatalf("empty case:\n%s", v)
	}
}

func TestSkillSuggest(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "ok"})
	defer srv.Close()
	project := map[string]string{
		"greet": "---\ndescription: Say hi SECRET123\n---\nHello.\n",
		"help":  "---\ndescription: SHADOWDESC\n---\nS.\n",
	}
	m, _ := skillsModel(t, srv, project, nil, nil)
	m = typeRunes(m, "/gr")
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "/greet (Say hi [redacted])") {
		t.Fatalf("no skill suggestion:\n%s", v)
	}
	if strings.Contains(v, "SECRET123") {
		t.Fatalf("secret in suggestion:\n%s", v)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := next.(Model).input.Value(); got != "/greet" {
		t.Fatalf("Tab gave %q", got)
	}

	m2, _ := skillsModel(t, srv, project, nil, nil)
	v = ansi.Strip(typeRunes(m2, "/he").View())
	if strings.Contains(v, "SHADOWDESC") || strings.Count(v, "/help") > 1 {
		t.Fatalf("shadowed skill suggested:\n%s", v)
	}
}
