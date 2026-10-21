package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
)

const agentsMainPrompt = "go spawn"

// agentsModel builds a session model in auto mode whose main agent spawns one
// subagent (task TASK-SUB ...) and then waits for it. Subagent requests (first
// user message = task) get sub; the main agent's last step is plain text.
func agentsModel(t *testing.T, sub providertest.Response) (Model, *agent.Session) {
	t.Helper()
	srv := providertest.NewFunc(func(_ int, req providertest.Request) providertest.Response {
		first := ""
		for _, msg := range req.Messages {
			if msg.Role == "user" {
				first = msg.Content
				break
			}
		}
		if first != agentsMainPrompt {
			return sub
		}
		tools := 0
		for _, msg := range req.Messages {
			if msg.Role == "tool" {
				tools++
			}
		}
		switch tools {
		case 0:
			return providertest.Response{ToolCalls: []providertest.ToolCall{
				{ID: "s1", Name: "spawn_agent", Arguments: `{"task":"TASK-SUB investigate the flux capacitor thoroughly and report back"}`}}}
		case 1:
			return providertest.Response{ToolCalls: []providertest.ToolCall{
				{ID: "w1", Name: "wait_agent", Arguments: `{"id":"a1"}`}}}
		}
		return providertest.Response{Text: "main done"}
	})
	t.Cleanup(srv.Close)
	m, sess := newSessionModel(t, srv)
	sess.SetMode(agent.ModeAuto)
	return m, sess
}

func waitSubStatus(t *testing.T, sess *agent.Session, id string, st agent.SubStatus) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		for _, s := range sess.Subagents() {
			if s.ID == id && s.Status == st {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("subagent %s never reached %s: %+v", id, st, sess.Subagents())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

var slowSub = providertest.Response{Text: "late", Delay: 30 * time.Second}

func TestAgentsList(t *testing.T) {
	m, sess := agentsModel(t, providertest.Response{
		Text: "sub finished", Usage: &providertest.Usage{Prompt: 40, Completion: 2}})
	m, _ = m.Command(agentsMainPrompt)
	waitSession(t, sess)
	m = m.Drain()
	subs := sess.Subagents()
	if len(subs) != 1 || subs[0].ID != "a1" || subs[0].Tokens == 0 {
		t.Fatalf("subagents = %+v", subs)
	}
	m, _ = m.Command("/agents")
	v := m.View()
	var row string
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "TASK-SUB") {
			row = l
		}
	}
	if row == "" {
		t.Fatalf("no table row:\n%s", v)
	}
	for _, want := range []string{"a1", "done", fmt.Sprint(subs[0].Tokens), "TASK-SUB investigate"} {
		if !strings.Contains(row, want) {
			t.Errorf("row %q lacks %q", row, want)
		}
	}
	if strings.Contains(row, "thoroughly and report back") {
		t.Errorf("task not shortened: %q", row)
	}
}

func TestAgentsTranscript(t *testing.T) {
	m, sess := agentsModel(t, providertest.Response{Text: "sub-final-answer SECRET123"})
	m, _ = m.Command(agentsMainPrompt)
	waitSession(t, sess)
	m = m.Drain()
	m, _ = m.Command("/agents a1")
	v := m.View()
	if !strings.Contains(v, "final: sub-final-answer") {
		t.Errorf("transcript missing:\n%s", v)
	}
	if strings.Contains(v, "SECRET123") {
		t.Errorf("transcript leaks the key:\n%s", v)
	}
}

func TestAgentsKill(t *testing.T) {
	m, sess := agentsModel(t, slowSub)
	m, _ = m.Command(agentsMainPrompt)
	waitSubStatus(t, sess, "a1", agent.SubRunning)
	m, _ = m.Command("/agents kill a1")
	if v := m.View(); !strings.Contains(v, "cancelled a1") {
		t.Errorf("no cancel confirmation:\n%s", v)
	}
	waitSubStatus(t, sess, "a1", agent.SubCancelled)
	waitSession(t, sess)
	m = m.Drain()
	m, _ = m.Command("/agents")
	if v := m.View(); !strings.Contains(v, "cancelled") || !strings.Contains(v, "a1") {
		t.Errorf("table lacks the cancelled subagent:\n%s", v)
	}
}

func TestAgentsUnknown(t *testing.T) {
	m, sess := agentsModel(t, providertest.Response{Text: "x"})
	m, _ = m.Command(agentsMainPrompt)
	waitSession(t, sess)
	for _, line := range []string{"/agents a9", "/agents kill a9"} {
		m, _ = m.Command(line)
		if v := m.View(); !strings.Contains(v, "error: unknown subagent: a9") {
			t.Errorf("%s: view:\n%s", line, v)
		}
	}
	// No session at all behaves the same.
	m2 := New(Options{StartDir: "/w"})
	m2, _ = m2.Command("/agents kill a9")
	if v := m2.View(); !strings.Contains(v, "error:") {
		t.Errorf("nil session kill:\n%s", v)
	}
}

func TestAgentsNone(t *testing.T) {
	m, _ := newSessionModel(t, nil)
	m, _ = m.Command("/agents")
	if v := m.View(); !strings.Contains(v, "no subagents") {
		t.Errorf("view:\n%s", v)
	}
	m2 := New(Options{StartDir: "/w"})
	m2, _ = m2.Command("/agents")
	if v := m2.View(); !strings.Contains(v, "no subagents") {
		t.Errorf("nil session view:\n%s", v)
	}
}

func TestSubagentPanel(t *testing.T) {
	m, sess := agentsModel(t, slowSub)
	before := len(strings.Split(m.View(), "\n"))
	m, _ = m.Command(agentsMainPrompt)
	waitSubStatus(t, sess, "a1", agent.SubRunning)
	m = m.Drain()
	v := m.View()
	var line string
	for _, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "● a1 TASK-SUB") {
			line = l
		}
	}
	if line == "" || !strings.Contains(line, "TASK-SUB investigate") || !strings.Contains(line, "· 0 tokens)") {
		t.Fatalf("panel line missing or wrong (%q):\n%s", line, v)
	}
	if got := len(strings.Split(v, "\n")); got != before {
		t.Errorf("panel changed the view height: %d -> %d", before, got)
	}
	// The panel sits between the conversation and the input box.
	if strings.LastIndex(v, "● a1 TASK-SUB") > strings.LastIndex(v, "╭") {
		t.Errorf("panel is not above the input:\n%s", v)
	}
	if err := sess.CancelSubagent("a1"); err != nil {
		t.Fatal(err)
	}
	waitSubStatus(t, sess, "a1", agent.SubCancelled)
	waitSession(t, sess)
	m = m.Drain()
	if v := m.View(); strings.Contains(v, "● a1 TASK-SUB") {
		t.Errorf("panel still shown after the subagent ended:\n%s", v)
	}
}
