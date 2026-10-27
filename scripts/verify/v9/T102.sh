#!/usr/bin/env bash
# Verification for T102 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
. "$(dirname "$0")/_probe.sh"

probe_src t102 <<'GO'
package tui

import (
	"strings"
	"testing"

	"yipyip/internal/agent"
)

func TestZZVerifyT102(t *testing.T) {
	m := feed(newBlockModel(), agent.Event{Kind: agent.EvText, AgentID: "main", Text: "# Big Title\n\nplain **bold** words"}).(Model)
	v := plainView(m)
	for _, want := range []string{"● Big Title", "  plain bold words"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "**") || strings.Contains(v, "# Big") {
		t.Errorf("markup left in the view:\n%s", v)
	}
	m2, _ := newSessionModel(t, answerServer(t, "key SECRET123 and **bold**"))
	m2 = tabTurn(t, m2, "q")
	v2 := plainView(m2)
	if strings.Contains(v2, "SECRET123") || !strings.Contains(v2, "key [redacted] and bold") {
		t.Errorf("redacted render wrong:\n%s", v2)
	}
}

func TestZZVerifyT102Neg(t *testing.T) {
	m, _ := newSessionModel(t, answerServer(t, "joined **SEC**RET123 here"))
	m = tabTurn(t, m, "q")
	v := plainView(m)
	if strings.Contains(v, "SECRET123") {
		t.Logf("the render reassembled the secret:\n%s", v)
		return
	}
	if !strings.Contains(v, "joined **SEC**RET123 here") {
		t.Fatalf("the fallback does not show the redacted source:\n%s", v)
	}
	t.Fatal("REJECTED: a render that joins a secret falls back to the redacted source")
}

func TestZZVerifyT102Raw(t *testing.T) {
	m := feed(newBlockModel(),
		agent.Event{Kind: agent.EvToolCall, AgentID: "main", Tool: "read_file", Args: `{"path":"a.md"}`},
		agent.Event{Kind: agent.EvToolResult, AgentID: "main", Tool: "read_file", Text: "# tool **raw**"},
		agent.Event{Kind: agent.EvInfo, Text: "info **raw**"}).(Model)
	v := plainView(m)
	if !strings.Contains(v, "# tool **raw**") || !strings.Contains(v, "info **raw**") {
		t.Logf("a tool result or info line was rendered as markdown:\n%s", v)
		return
	}
	t.Fatal("REJECTED: tool results and info lines keep their markup")
}
GO

expect_ok "T102 tests" <<'CMD'
go_tests ./internal/tui TestMarkdownSkeletonView TestMarkdownSkeletonOtherBlocksRaw TestMarkdownSecretReassembly TestRenderAssistantBlock TestFenceHighlightedInView TestCopySecretNeverCopied
CMD
expect_ok "phase 1 exit: -run 'MarkdownSkeleton|Render'" <<'CMD'
go test ./internal/tui -run 'MarkdownSkeleton|Render' -count=1
CMD
expect_ok "blocks.go calls renderMarkdown" <<'CMD'
grep -n -e 'renderMarkdown(' internal/tui/blocks.go
CMD
expect_ok "probe: assistant block rendered after redaction" <<'CMD'
go_probe internal/tui t102 TestZZVerifyT102
CMD
expect_fail "negative: a render that joins a secret is shown" <<'CMD'
go_probe_rejects internal/tui t102 TestZZVerifyT102Neg
CMD
expect_fail "negative: tool results or info lines rendered as markdown" <<'CMD'
go_probe_rejects internal/tui t102 TestZZVerifyT102Raw
CMD
expect_ok "security files and go.mod unchanged" <<'CMD'
v9_guard
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
