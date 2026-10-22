package providertest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func jevPost(t *testing.T, ctx context.Context, url, body string) (int, string, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer K1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), nil
}

func TestJevFakeEndpoint(t *testing.T) {
	s := NewJev(func(n int, _ JevRequest) JevResponse {
		switch n {
		case 0:
			return JevResponse{Answers: map[string]any{"c": JevChoiceAnswer("a", 0.5), "s": JevScoreAnswer(3, 0.25), "n": JevNoulAnswer(0.75)}, Usage: &JevUsage{Input: 7, Output: 2}}
		case 1:
			return JevResponse{Status: 422}
		case 2:
			return JevResponse{Status: 500, Body: "verbatim"}
		default:
			return JevResponse{Delay: 5 * time.Second}
		}
	})
	defer s.Close()
	ctx := context.Background()
	body := `{"model":"m","state":"st","questions":{"c":{"type":"choice"}}}`

	code, raw, err := jevPost(t, ctx, s.URL+"/v1/systemone", body)
	if err != nil || code != 200 {
		t.Fatalf("success: %d %v", code, err)
	}
	var js map[string]any
	if err := json.Unmarshal([]byte(raw), &js); err != nil {
		t.Fatal(err)
	}
	ans := js["answers"].(map[string]any)
	c, sc, n := ans["c"].(map[string]any), ans["s"].(map[string]any), ans["n"].(map[string]any)
	if js["model"] != "m" || c["choice"] != "a" || c["confidence"] != 0.5 || sc["score"] != 3.0 || sc["type"] != "score" || n["noul"] != 0.75 || n["type"] != "noul" {
		t.Fatalf("body: %s", raw)
	}
	if u := js["usage"].(map[string]any); u["input_tokens"] != 7.0 || u["output_tokens"] != 2.0 {
		t.Fatalf("usage: %s", raw)
	}

	if code, raw, _ = jevPost(t, ctx, s.URL+"/v1/systemone", body); code != 422 || !strings.Contains(raw, `"detail"`) {
		t.Fatalf("422: %d %s", code, raw)
	}
	if code, raw, _ = jevPost(t, ctx, s.URL+"/v1/systemone", body); code != 500 || raw != "verbatim" {
		t.Fatalf("verbatim: %d %q", code, raw)
	}
	if code, _, _ = jevPost(t, ctx, s.URL+"/v1/other", body); code != 404 {
		t.Fatalf("other path: %d", code)
	}

	cctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, _, err := jevPost(t, cctx, s.URL+"/v1/systemone", body); err == nil {
		t.Fatal("delayed reply not cut")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("delay not cut early")
	}

	reqs := s.Requests()
	if len(reqs) != 4 {
		t.Fatalf("recorded %d, want 4", len(reqs))
	}
	r := reqs[0]
	if r.Method != "POST" || r.Path != "/v1/systemone" || r.Header.Get("Authorization") != "Bearer K1" || r.Model != "m" || r.State != "st" || r.Questions["c"] == nil || string(r.Body) != body {
		t.Fatalf("recorded: %+v", r)
	}
}
