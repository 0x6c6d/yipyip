package providertest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

// JevUsage is the "usage" object of a reply.
type JevUsage struct{ Input, Output int } // "input_tokens", "output_tokens"

// JevRequest is one recorded request.
type JevRequest struct {
	Method, Path string
	Header       http.Header
	Body         []byte
	JSON         map[string]any // decoded body (nil if not JSON)
	Model        string         // "model"
	State        string         // "state"
	Questions    map[string]any // "questions" (name -> question object)
}

// JevResponse scripts one reply of the fake System One endpoint.
type JevResponse struct {
	Status  int            // 0 or 200 = success
	Body    string         // non-empty: sent verbatim with Status (or 200)
	Answers map[string]any // success body "answers" (name -> answer object)
	Usage   *JevUsage      // success body "usage" when non-nil, omitted otherwise
	Delay   time.Duration  // wait before replying; ends early when the request is cancelled
}

// JevServer is a running fake System One (jev) endpoint.
type JevServer struct {
	URL string // "http://127.0.0.1:<port>", no path

	ts   *httptest.Server
	fn   func(n int, req JevRequest) JevResponse
	mu   sync.Mutex
	reqs []JevRequest
}

// NewJev starts a fake endpoint whose reply to the n-th (0-based) request is fn(n, req).
func NewJev(fn func(n int, req JevRequest) JevResponse) *JevServer {
	s := &JevServer{fn: fn}
	s.ts = httptest.NewServer(http.HandlerFunc(s.handle))
	s.URL = s.ts.URL
	return s
}

// Requests returns a copy of the recorded requests in arrival order.
func (s *JevServer) Requests() []JevRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]JevRequest(nil), s.reqs...)
}

// Close shuts the server down.
func (s *JevServer) Close() {
	s.ts.CloseClientConnections()
	s.ts.Close()
}

// JevChoiceAnswer is a "choice" answer.
func JevChoiceAnswer(choice string, confidence float64) map[string]any {
	return map[string]any{"type": "choice", "choice": choice, "confidence": confidence}
}

// JevScoreAnswer is a "score" answer.
func JevScoreAnswer(score, confidence float64) map[string]any {
	return map[string]any{"type": "score", "score": score, "confidence": confidence}
}

// JevNoulAnswer is a "noul" answer with probability p.
func JevNoulAnswer(p float64) map[string]any {
	return map[string]any{"type": "noul", "noul": p}
}

func (s *JevServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/systemone" {
		http.NotFound(w, r)
		return
	}
	body, _ := io.ReadAll(r.Body)
	req := JevRequest{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone(), Body: body}
	var m map[string]any
	if json.Unmarshal(body, &m) == nil && m != nil {
		req.JSON = m
		req.Model, _ = m["model"].(string)
		req.State, _ = m["state"].(string)
		req.Questions, _ = m["questions"].(map[string]any)
	}
	s.mu.Lock()
	n := len(s.reqs)
	s.reqs = append(s.reqs, req)
	s.mu.Unlock()

	resp := s.fn(n, req)
	if resp.Delay > 0 {
		t := time.NewTimer(resp.Delay)
		select {
		case <-t.C:
		case <-r.Context().Done():
			t.Stop()
			return
		}
	}
	status := resp.Status
	if status == 0 {
		status = http.StatusOK
	}
	if resp.Body != "" {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, resp.Body)
		return
	}
	switch {
	case status == http.StatusOK:
		out := map[string]any{"model": req.Model, "answers": resp.Answers}
		if resp.Usage != nil {
			out["usage"] = map[string]any{"input_tokens": resp.Usage.Input, "output_tokens": resp.Usage.Output}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	case status == http.StatusUnprocessableEntity:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"detail":[{"loc":["body","questions"],"msg":"invalid question","type":"value_error"}]}`)
	default:
		w.WriteHeader(status)
		_, _ = io.WriteString(w, "jev fake error")
	}
}
