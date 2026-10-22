package providertest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"time"
)

// BraveResult is one web result of the fake Brave endpoint.
type BraveResult struct{ Title, URL, Description string }

// BraveRequest is one recorded request.
type BraveRequest struct {
	Method, Path, RawQuery string
	Query                  url.Values
	Header                 http.Header
}

// BraveResponse scripts one reply of the fake Brave endpoint.
type BraveResponse struct {
	Status  int           // 0 or 200 = success
	Body    string        // non-empty: sent verbatim with Status (or 200)
	Results []BraveResult // success body results
	Delay   time.Duration // wait before replying; ends early when the request is cancelled
}

// BraveServer is a running fake Brave search endpoint.
type BraveServer struct {
	URL string // "http://127.0.0.1:<port>", no path

	ts   *httptest.Server
	fn   func(n int, req BraveRequest) BraveResponse
	mu   sync.Mutex
	reqs []BraveRequest
}

// NewBrave starts a fake endpoint whose reply to the n-th (0-based) request is fn(n, req).
func NewBrave(fn func(n int, req BraveRequest) BraveResponse) *BraveServer {
	s := &BraveServer{fn: fn}
	s.ts = httptest.NewServer(http.HandlerFunc(s.handle))
	s.URL = s.ts.URL
	return s
}

// Requests returns a copy of the recorded requests in arrival order.
func (s *BraveServer) Requests() []BraveRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]BraveRequest(nil), s.reqs...)
}

// Close shuts the server down.
func (s *BraveServer) Close() {
	s.ts.CloseClientConnections()
	s.ts.Close()
}

func (s *BraveServer) handle(w http.ResponseWriter, r *http.Request) {
	req := BraveRequest{
		Method:   r.Method,
		Path:     r.URL.Path,
		RawQuery: r.URL.RawQuery,
		Query:    r.URL.Query(),
		Header:   r.Header.Clone(),
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
	type item struct {
		Title       string `json:"title"`
		URL         string `json:"url"`
		Description string `json:"description"`
	}
	items := make([]item, 0, len(resp.Results))
	for _, r := range resp.Results {
		items = append(items, item{r.Title, r.URL, r.Description})
	}
	out := map[string]any{"type": "search", "web": map[string]any{"results": items}}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(out)
}
