package agent

import (
	"strings"
	"testing"
	"time"

	"yipyip/internal/provider/providertest"
)

const riskBashArgs = `{"command":"echo hi SECRET123"}`

// riskRun is the outcome of one risk-screen turn.
type riskRun struct {
	approvals int
	result    string // last result of the scripted tool
	infos     []string
	jev       []providertest.JevRequest
	s         *Session
}

// runRisk runs one turn whose model calls tool with args once, in mode, with
// jev on and answering resp (its "risk" answer is sent under every asked
// question name); every approval prompt is answered with dec.
func runRisk(t *testing.T, tool, args string, resp providertest.JevResponse, mode Mode, dec Decision) riskRun {
	t.Helper()
	srv := providertest.NewFunc(func(_ int, req providertest.Request) providertest.Response {
		if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "user" {
			return providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "k1", Name: tool, Arguments: args}}}
		}
		return providertest.Response{Text: "done"}
	})
	t.Cleanup(srv.Close)
	j := providertest.NewJev(func(_ int, req providertest.JevRequest) providertest.JevResponse {
		out := resp
		if a, ok := resp.Answers["risk"]; ok {
			out.Answers = map[string]any{}
			for name := range req.Questions {
				out.Answers[name] = a
			}
		}
		return out
	})
	t.Cleanup(j.Close)
	s := newJevSession(t, srv, `{"baseURL":"`+j.URL+`","apiKey":"{env:TYPESAFE_API_KEY}"}`)
	s.SetJevTimeout(150 * time.Millisecond)
	s.SetMode(mode)
	if err := s.SetJev(true); err != nil {
		t.Fatal(err)
	}
	if err := s.Submit("go"); err != nil {
		t.Fatal(err)
	}
	r := riskRun{s: s}
	deadline := time.Now().Add(8 * time.Second)
	for done := false; !done; {
		for _, ev := range s.PollEvents() {
			switch ev.Kind {
			case EvApproval:
				r.approvals++
				ev.Approval.Respond(dec)
			case EvToolResult:
				if ev.Tool == tool {
					r.result = ev.Text
				}
			case EvInfo:
				r.infos = append(r.infos, ev.Text)
			case EvTurnDone:
				done = true
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("turn did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.jev = j.Requests()
	return r
}

func riskNoul(p float64) providertest.JevResponse {
	return providertest.JevResponse{Answers: map[string]any{"risk": providertest.JevNoulAnswer(p)}}
}

func TestJevRisk(t *testing.T) {
	r := runRisk(t, "bash", riskBashArgs, riskNoul(0.9), ModeAuto, Deny)
	if r.approvals != 1 || r.result != DeniedMessage || len(r.jev) != 1 {
		t.Fatalf("deny: %d approvals, result %q, %d jev requests", r.approvals, r.result, len(r.jev))
	}
	if !contains(r.infos, "jev: risky bash call, asking for approval") {
		t.Fatalf("infos %q", r.infos)
	}
	if st := r.s.JevStats(); st.Answered != 1 || st.Fallbacks != 0 {
		t.Fatalf("stats %+v", st)
	}
	if r.s.Mode() != ModeAuto {
		t.Fatalf("mode changed to %v", r.s.Mode())
	}
	r = runRisk(t, "bash", riskBashArgs, riskNoul(0.9), ModeAuto, Allow)
	if r.approvals != 1 || !strings.Contains(r.result, "hi") {
		t.Fatalf("allow: %d approvals, result %q", r.approvals, r.result)
	}
}

func TestJevRiskSafeUnchanged(t *testing.T) {
	r := runRisk(t, "bash", riskBashArgs, riskNoul(0.1), ModeAuto, Deny)
	if r.approvals != 0 || !strings.Contains(r.result, "hi") || len(r.jev) != 1 {
		t.Fatalf("%d approvals, result %q, %d jev requests", r.approvals, r.result, len(r.jev))
	}
	if st := r.s.JevStats(); st.Answered != 1 || st.Fallbacks != 0 {
		t.Fatalf("stats %+v", st)
	}
}

func TestJevRiskManualUnchanged(t *testing.T) {
	for _, p := range []float64{0, 0.9} {
		r := runRisk(t, "bash", riskBashArgs, riskNoul(p), ModeManual, Deny)
		if r.approvals != 1 || r.result != DeniedMessage || len(r.jev) != 0 {
			t.Fatalf("p=%v: %d approvals, result %q, %d jev requests", p, r.approvals, r.result, len(r.jev))
		}
	}
}

func TestJevRiskFallback(t *testing.T) {
	for name, resp := range map[string]providertest.JevResponse{
		"500":     {Status: 500},
		"timeout": {Delay: 5 * time.Second},
	} {
		r := runRisk(t, "bash", riskBashArgs, resp, ModeAuto, Deny)
		if r.approvals != 0 || !strings.Contains(r.result, "hi") {
			t.Fatalf("%s: %d approvals, result %q", name, r.approvals, r.result)
		}
		if st := r.s.JevStats(); st.Fallbacks != 1 || st.Answered != 0 {
			t.Fatalf("%s: stats %+v", name, st)
		}
	}
}

func TestJevRiskState(t *testing.T) {
	r := runRisk(t, "bash", riskBashArgs, riskNoul(0.1), ModeAuto, Deny)
	if len(r.jev) != 1 {
		t.Fatalf("%d jev requests", len(r.jev))
	}
	st := r.jev[0].State
	if !strings.HasPrefix(st, "tool: bash\nargs: ") || !strings.Contains(st, "echo hi") || r.jev[0].Questions["risk"] == nil {
		t.Fatalf("state %q, questions %v", st, r.jev[0].Questions)
	}
	if strings.Contains(string(r.jev[0].Body), "SECRET123") {
		t.Fatalf("secret in jev request: %s", r.jev[0].Body)
	}
	r = runRisk(t, "read_file", `{"path":"notes.txt"}`, riskNoul(0.9), ModeAuto, Deny)
	if len(r.jev) != 0 || r.approvals != 0 {
		t.Fatalf("read_file: %d jev requests, %d approvals", len(r.jev), r.approvals)
	}
}
