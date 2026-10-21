package tui

import (
	"strings"
	"testing"
	"time"

	"yipyip/internal/agent"
	"yipyip/internal/provider/providertest"
)

func retryMsg(sess *agent.Session) EventMsg {
	info := agent.RetryInfo{Attempt: 2, Max: 10, Wait: time.Second}
	return EventMsg{Event: agent.Event{Kind: agent.EvRetry, AgentID: agent.MainAgentID, Text: "retrying (2/10) in 1s", Retry: &info}, sess: sess}
}

func TestRetryStatusShown(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	if strings.Contains(plainView(m), "retrying") {
		t.Fatal("retry status shown before any retry")
	}
	next, _ := m.Update(retryMsg(sess))
	m = next.(Model)
	if v := plainView(m); !strings.Contains(v, "retrying (2/10) in 1s") {
		t.Errorf("view lacks the retry status:\n%s", v)
	}
	if len(m.blocks) != 0 {
		t.Errorf("retry status added %d blocks to the conversation", len(m.blocks))
	}
}

func TestRetryStatusClearedOnDelta(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	m, sess := newSessionModel(t, srv)
	next, _ := m.Update(retryMsg(sess))
	m = next.(Model)
	if !strings.Contains(plainView(m), "retrying (2/10) in 1s") {
		t.Fatal("retry status not shown")
	}
	next, _ = m.Update(EventMsg{Event: agent.Event{Kind: agent.EvText, AgentID: agent.MainAgentID, Text: "hello"}, sess: sess})
	m = next.(Model)
	if v := plainView(m); strings.Contains(v, "retrying") {
		t.Errorf("retry status still shown after a delta:\n%s", v)
	}
}

func TestRetryStatusPerTab(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	m, sess0 := newSessionModel(t, srv)
	mustAddTab(t, &m) // tab 1 is current, tab 0 is in the background
	next, _ := m.Update(retryMsg(sess0))
	m = next.(Model)
	if v := belowTabBar(plainView(m)); strings.Contains(v, "retrying") {
		t.Errorf("background tab's retry status shown in the current tab:\n%s", v)
	}
	m.switchTab(0)
	if v := plainView(m); !strings.Contains(v, "retrying (2/10) in 1s") {
		t.Errorf("tab 0 lost its retry status:\n%s", v)
	}
	m.switchTab(1)
	if v := belowTabBar(plainView(m)); strings.Contains(v, "retrying") {
		t.Errorf("retry status leaked into tab 1:\n%s", v)
	}
}
