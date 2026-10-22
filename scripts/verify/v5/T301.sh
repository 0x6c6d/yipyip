#!/usr/bin/env bash
# Verification for T301 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named tests (agent, tui) and adapted v4 jev tests" <<'CMD'
go_tests ./internal/agent TestJevStatsCounters TestJevStatsTokens TestJevRoute TestRouteTiers && go_tests ./internal/tui TestJevStatsLines TestJevStatsNotConfigured TestJevStats TestJevToggle
CMD
expect_ok "phase 3 exit criterion (stats part)" <<'CMD'
! grep -rn QuestionTokens internal && go test -count=1 ./internal/agent/ ./internal/tui/ -run 'Jev'
CMD
expect_ok "write supervisor probes (overlay tests, nothing written to the repo)" <<'CMD'
cat > "$VTMP/agent_probe_test.go" <<'EOF'
package agent

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
)

func zzSess(t *testing.T, srv *providertest.Server, jevJSON string) *Session {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, dir := filepath.Join(base, "home"), filepath.Join(base, "start")
	for _, d := range []string{filepath.Join(home, ".config", "yipyip"), dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(config.GlobalPath(home), []byte(providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 100000)), 0o644); err != nil {
		t.Fatal(err)
	}
	proj := `{"provider":{"fake":{"models":{"m3":{"name":"m3"},"m4":{"name":"m4"}}}},"jev":` + jevJSON + `}`
	if err := os.WriteFile(filepath.Join(dir, config.ProjectFile), []byte(proj), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(home, dir, func(k string) string {
		switch k {
		case "FAKE_KEY":
			return "SECRET123"
		case "TYPESAFE_API_KEY":
			return "JEVKEY-zz"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSession(Options{Config: cfg, StartDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Cancel(); s.Wait() })
	return s
}

// zzJev answers by the user text in the state: t1xx easy, t2xx medium,
// t3xx hard, t4xx HTTP 500, anything else easy.
func zzJev(t *testing.T) *providertest.JevServer {
	j := providertest.NewJev(func(_ int, req providertest.JevRequest) providertest.JevResponse {
		score := 0.0
		switch {
		case strings.Contains(req.State, "t2"):
			score = 1
		case strings.Contains(req.State, "t3"):
			score = 2
		case strings.Contains(req.State, "t4"):
			return providertest.JevResponse{Status: 500}
		}
		ans := map[string]any{}
		for name := range req.Questions {
			ans[name] = providertest.JevScoreAnswer(score, 0.9)
		}
		return providertest.JevResponse{Answers: ans}
	})
	t.Cleanup(j.Close)
	return j
}

func zzUser(req providertest.Request) (string, int) {
	user, steps := "", 0
	for _, m := range req.Messages {
		switch m.Role {
		case "user":
			user, steps = m.Content, 0
		case "tool":
			steps++
		}
	}
	return user, steps
}

func zzWaitTurn(t *testing.T, s *Session, text string) {
	t.Helper()
	if err := s.Submit(text); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for done := false; !done; {
		for _, ev := range s.PollEvents() {
			switch ev.Kind {
			case EvApproval:
				ev.Approval.Respond(Allow)
			case EvTurnDone:
				done = true
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("turn did not finish")
		}
		time.Sleep(3 * time.Millisecond)
	}
}

const zzRoute3 = `{"easy":"fake/m2","medium":"fake/m3","hard":"fake/m4"}`

func TestZZPosCounters(t *testing.T) {
	// "tNNN": one request with NNN total tokens.
	srv := providertest.NewFunc(func(_ int, req providertest.Request) providertest.Response {
		user, _ := zzUser(req)
		n, _ := strconv.Atoi(strings.TrimPrefix(user, "t"))
		return providertest.Response{Text: "ok", Usage: &providertest.Usage{Prompt: n - 10, Completion: 10}}
	})
	t.Cleanup(srv.Close)
	j := zzJev(t)
	s := zzSess(t, srv, `{"baseURL":"`+j.URL+`","apiKey":"{env:TYPESAFE_API_KEY}","route":`+zzRoute3+`}`)
	s.SetJevTimeout(500 * time.Millisecond)
	if err := s.SetJev(true); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"t100", "t200", "t300", "t400", "t101"} {
		zzWaitTurn(t, s, u)
	}
	if err := s.SetJev(false); err != nil {
		t.Fatal(err)
	}
	zzWaitTurn(t, s, "t500")
	st := s.JevStats()
	if st.Turns != [4]int{2, 2, 1, 1} || st.MainTokens != [4]int{900, 201, 200, 300} || st.Fallbacks != 1 || st.InputTokens <= 0 {
		t.Fatalf("stats %+v", st)
	}
	if TierNone != 0 || TierEasy != 1 || TierMedium != 2 || TierHard != 3 {
		t.Fatal("tier indices")
	}
}

func TestZZPosSubagentTokens(t *testing.T) {
	srv := providertest.NewFunc(func(_ int, req providertest.Request) providertest.Response {
		user, steps := zzUser(req)
		one := &providertest.Usage{Prompt: 1}
		if user == "SUBTASK-zz" {
			return providertest.Response{Text: "sub done", Usage: &providertest.Usage{Prompt: 990, Completion: 10}}
		}
		switch steps {
		case 0:
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "s1", Name: "spawn_agent", Arguments: `{"task":"SUBTASK-zz"}`}}, Usage: one}
		case 1:
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "w1", Name: "wait_agent", Arguments: `{"id":"a1"}`}}, Usage: one}
		}
		return providertest.Response{Text: "main done", Usage: one}
	})
	t.Cleanup(srv.Close)
	j := zzJev(t)
	s := zzSess(t, srv, `{"baseURL":"`+j.URL+`","apiKey":"{env:TYPESAFE_API_KEY}","route":`+zzRoute3+`}`)
	s.SetJevTimeout(500 * time.Millisecond)
	s.SetMode(ModeAuto)
	if err := s.SetJev(true); err != nil {
		t.Fatal(err)
	}
	zzWaitTurn(t, s, "t1 spawn please")
	st := s.JevStats()
	if st.Turns != [4]int{0, 1, 0, 0} || st.MainTokens != [4]int{0, 1003, 0, 0} {
		t.Fatalf("subagent tokens not under easy: %+v", st)
	}
}
EOF
cat > "$VTMP/tui_probe_test.go" <<'EOF'
package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
)

func zzModel(t *testing.T, srv *providertest.Server, project string) (Model, *agent.Session) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, dir := filepath.Join(base, "home"), filepath.Join(base, "start")
	for _, d := range []string{filepath.Join(home, ".config", "yipyip"), dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(config.GlobalPath(home), []byte(providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 100000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if project != "" {
		if err := os.WriteFile(filepath.Join(dir, config.ProjectFile), []byte(project), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(home, dir, func(k string) string {
		switch k {
		case "FAKE_KEY":
			return "SECRET123"
		case "TYPESAFE_API_KEY":
			return "JEVKEY-zz"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := agent.NewSession(agent.Options{Config: cfg, StartDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Cancel(); sess.Wait() })
	m := New(Options{StartDir: dir, Session: sess})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	return next.(Model), sess
}

func zzRun(t *testing.T, m Model, sess *agent.Session, line string) Model {
	t.Helper()
	m, _ = m.Command(line)
	done := make(chan struct{})
	go func() { sess.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("turn did not finish")
	}
	return m.Drain()
}

// zzStats checks that the view holds the 11 lines want, in order, consecutive.
func zzStats(t *testing.T, m Model, want []string) {
	t.Helper()
	view := ansi.Strip(m.View())
	lines := strings.Split(view, "\n")
	match := func(l, w string) bool {
		l = strings.TrimSpace(l)
		if w == "jev input tokens: >0" {
			i := strings.Index(l, "jev input tokens: ")
			n, err := strconv.Atoi(strings.TrimSpace(l[max(i, 0)+len("jev input tokens: "):]))
			return i >= 0 && err == nil && n > 0
		}
		return l == w || strings.HasSuffix(l, " "+w)
	}
	for i := range lines {
		if !match(lines[i], want[0]) {
			continue
		}
		ok := i+len(want) <= len(lines)
		for k := 1; ok && k < len(want); k++ {
			ok = match(lines[i+k], want[k])
		}
		if ok {
			if strings.Contains(strings.Join(lines[i:i+len(want)], "\n"), "question") {
				t.Fatalf("question line in stats:\n%s", view)
			}
			return
		}
	}
	t.Fatalf("stats lines %q not found in:\n%s", want, view)
}

func TestZZPosStatsLines(t *testing.T) {
	srv := providertest.NewFunc(func(_ int, req providertest.Request) providertest.Response {
		user := ""
		for _, m := range req.Messages {
			if m.Role == "user" {
				user = m.Content
			}
		}
		n, _ := strconv.Atoi(strings.TrimPrefix(user, "t"))
		return providertest.Response{Text: "ok", Usage: &providertest.Usage{Prompt: n - 10, Completion: 10}}
	})
	t.Cleanup(srv.Close)
	j := providertest.NewJev(func(_ int, req providertest.JevRequest) providertest.JevResponse {
		score := 0.0
		switch {
		case strings.Contains(req.State, "t2"):
			score = 1
		case strings.Contains(req.State, "t3"):
			score = 2
		case strings.Contains(req.State, "t4"):
			return providertest.JevResponse{Status: 500}
		}
		ans := map[string]any{}
		for name := range req.Questions {
			ans[name] = providertest.JevScoreAnswer(score, 0.9)
		}
		return providertest.JevResponse{Answers: ans}
	})
	t.Cleanup(j.Close)
	proj := `{"provider":{"fake":{"models":{"m3":{"name":"m3"},"m4":{"name":"m4"}}}},"jev":{"baseURL":"` + j.URL +
		`","apiKey":"{env:TYPESAFE_API_KEY}","route":{"easy":"fake/m2","medium":"fake/m3","hard":"fake/m4"}}}`
	m, sess := zzModel(t, srv, proj)
	m, _ = m.Command("/jev stats")
	zzStats(t, m, []string{
		"turns routed easy: 0", "turns routed medium: 0", "turns routed hard: 0", "turns not routed: 0",
		"jev fallbacks: 0", "jev input tokens: 0", "main tokens easy: 0", "main tokens medium: 0",
		"main tokens hard: 0", "main tokens not routed: 0",
		"avg main tokens per turn: easy -, medium -, hard -, not routed -",
	})
	m, _ = m.Command("/jev")
	if !sess.JevOn() {
		t.Fatal("/jev did not switch jev on")
	}
	for _, u := range []string{"t100", "t200", "t300", "t400", "t101"} {
		m = zzRun(t, m, sess, u)
	}
	m, _ = m.Command("/jev")
	if sess.JevOn() {
		t.Fatal("/jev did not switch jev off")
	}
	m = zzRun(t, m, sess, "t500")
	m, _ = m.Command("/jev stats")
	zzStats(t, m, []string{
		"turns routed easy: 2", "turns routed medium: 1", "turns routed hard: 1", "turns not routed: 2",
		"jev fallbacks: 1", "jev input tokens: >0", "main tokens easy: 201", "main tokens medium: 200",
		"main tokens hard: 300", "main tokens not routed: 900",
		"avg main tokens per turn: easy 100, medium 200, hard 300, not routed 450",
	})
}

func TestZZPosNotConfigured(t *testing.T) {
	srv := providertest.New()
	t.Cleanup(srv.Close)
	m, _ := zzModel(t, srv, "")
	m, _ = m.Command("/jev stats")
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "jev is not configured") || strings.Contains(v, "turns routed") || strings.Contains(v, "main tokens") {
		t.Fatalf("not configured:\n%s", v)
	}
}
EOF
printf '{"Replace":{"%s":"%s","%s":"%s"}}' "$ROOT/internal/agent/zz_verify_probe_test.go" "$VTMP/agent_probe_test.go" "$ROOT/internal/tui/zz_verify_probe_test.go" "$VTMP/tui_probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "agent probe: per-tier turns and tokens, fallback, subagent tokens under the turn's tier" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/agent TestZZPosCounters TestZZPosSubagentTokens
CMD
expect_ok "tui probe: exact 11 lines (fresh and after scripted turns), not configured" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/tui TestZZPosStatsLines TestZZPosNotConfigured
CMD
expect_fail "negative: removed stats fields or categories still in internal" <<'CMD'
no_match -rn -e 'QuestionTokens' -e 'RoutingTokens' -e 'OtherTokens' -e 'Answered' -e 'main tokens question' -e 'jev answered:' internal
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet 0c3a7c5 -- go.mod go.sum
CMD
expect_fail "negative: no protected or security-critical path touched" <<'CMD'
out="$(git status --porcelain --untracked-files=all -- README.md Plan.md CLAUDE.md prompts .claude scripts/verify versions/v5/spec.md versions/v5/plan.md versions/v5/tasks internal/provider internal/tools internal/config internal/agent/approval.go internal/agent/approval_test.go internal/agent/question.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
