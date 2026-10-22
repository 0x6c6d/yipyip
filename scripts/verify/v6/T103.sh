#!/usr/bin/env bash
# Verification for T103 (supervisor-owned; workers never edit it). Exit 0 = pass.
. "$(dirname "$0")/../_lib.sh"
expect_ok "named search client tests" <<'CMD'
go_tests ./internal/provider TestSearchClientRequest TestSearchClientResults TestSearchClientNon200 TestSearchClientBadJSON TestSearchClientBodyLimit TestSearchClientCancel
CMD
expect_ok "whole provider package incl. providertest" <<'CMD'
go test -count=1 ./internal/provider/...
CMD
expect_ok "write supervisor probe (overlay test, nothing written to the repo)" <<'CMD'
cat > "$VTMP/probe_test.go" <<'EOF'
package provider_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"yipyip/internal/provider"
	"yipyip/internal/provider/providertest"
)

func zzClient(t *testing.T, b *providertest.BraveServer) *provider.SearchClient {
	t.Helper()
	hc, err := provider.NewSearchHTTPClient(b.URL)
	if err != nil {
		t.Fatal(err)
	}
	return provider.NewSearchClient(hc, b.URL, "BRAVEKEY-P")
}

func TestZZBravePos(t *testing.T) {
	b := providertest.NewBrave(func(n int, req providertest.BraveRequest) providertest.BraveResponse {
		if req.Query.Get("q") == "none" {
			return providertest.BraveResponse{}
		}
		return providertest.BraveResponse{Results: []providertest.BraveResult{
			{Title: "T1", URL: "https://a.example/1", Description: "<strong>go</strong> &amp; more"},
			{Title: "T2", URL: "https://b.example/2", Description: "two"},
		}}
	})
	defer b.Close()
	c := zzClient(t, b)
	res, err := c.Search(context.Background(), "go generics & more?", 3)
	if err != nil || len(res) != 2 || res[0].Title != "T1" || res[0].URL != "https://a.example/1" || res[0].Description != "<strong>go</strong> &amp; more" || res[1].Title != "T2" {
		t.Fatalf("results %+v err %v", res, err)
	}
	reqs := b.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests %d", len(reqs))
	}
	r := reqs[0]
	if r.Method != "GET" || r.Path != "/res/v1/web/search" || r.Query.Get("q") != "go generics & more?" || r.Query.Get("count") != "3" {
		t.Fatalf("request %+v", r)
	}
	if r.Header.Get("X-Subscription-Token") != "BRAVEKEY-P" || r.Header.Get("Accept") != "application/json" || r.Header.Get("Authorization") != "" {
		t.Fatalf("headers %v", r.Header)
	}
	res, err = c.Search(context.Background(), "none", 5)
	if err != nil || len(res) != 0 {
		t.Fatalf("no results: %+v %v", res, err)
	}
}

// TestZZBraveEvil passes (exit 0) only when a bad reply is not reported as
// prescribed or an error echoes the key or the body.
func TestZZBraveEvil(t *testing.T) {
	big := `{"web":{"results":[{"title":"` + strings.Repeat("a", 2<<20) + `","url":"u","description":"d"}]}}`
	b := providertest.NewBrave(func(n int, req providertest.BraveRequest) providertest.BraveResponse {
		switch req.Query.Get("q") {
		case "s500":
			return providertest.BraveResponse{Status: 500, Body: "BRAVEKEY-P upstream detail"}
		case "s401":
			return providertest.BraveResponse{Status: 401, Body: `{"error":"BRAVEKEY-P upstream detail"}`}
		case "bad":
			return providertest.BraveResponse{Body: "{nope"}
		case "big":
			return providertest.BraveResponse{Body: big}
		}
		return providertest.BraveResponse{}
	})
	defer b.Close()
	c := zzClient(t, b)
	for _, q := range []string{"s500", "s401"} {
		_, err := c.Search(context.Background(), q, 5)
		var se *provider.SearchStatusError
		if err == nil || !errors.As(err, &se) || (q == "s500" && se.Code != 500) || (q == "s401" && se.Code != 401) {
			t.Logf("%s: not a SearchStatusError: %v", q, err)
			return
		}
		if strings.Contains(err.Error(), "BRAVEKEY-P") || strings.Contains(err.Error(), "upstream detail") {
			t.Logf("%s: error echoes key or body: %v", q, err)
			return
		}
	}
	for _, q := range []string{"bad", "big"} {
		_, err := c.Search(context.Background(), q, 5)
		if err == nil || !errors.Is(err, provider.ErrSearchInvalidResponse) || strings.Contains(err.Error(), "BRAVEKEY-P") {
			t.Logf("%s: not ErrSearchInvalidResponse: %v", q, err)
			return
		}
	}
	t.Fatal("REJECTED: non-200, bad JSON and oversized bodies reported as prescribed")
}
EOF
printf '{"Replace":{"%s":"%s"}}' "$ROOT/internal/provider/zz_verify_probe_test.go" "$VTMP/probe_test.go" > "$VTMP/overlay.json"
CMD
expect_ok "probe: request path, query, count, headers; result mapping; no results" <<'CMD'
GOFLAGS="-overlay=$VTMP/overlay.json" go_tests ./internal/provider TestZZBravePos
CMD
expect_fail "negative: non-200, bad JSON or oversized body not reported as prescribed, or key/body echoed" <<'CMD'
out="$(go test -count=1 -overlay="$VTMP/overlay.json" -run '^TestZZBraveEvil$' ./internal/provider 2>&1)"
printf '%s\n' "$out"
printf '%s\n' "$out" | grep -q -e 'REJECTED:' && exit 1
printf '%s\n' "$out" | grep -q -e '^ok' && exit 0
exit 125
CMD
expect_fail "negative: a security-critical file differs from HEAD (diff)" <<'CMD'
out="$(git diff --name-only HEAD -- internal/provider/transport.go internal/tools/pathguard.go internal/agent/approval.go internal/tools/bash.go internal/config/redact.go)" || exit 125
printf '%s' "$out" | no_match -e .
CMD
expect_fail "negative: an http.Client is built outside transport.go" <<'CMD'
grep -rln --include='*.go' --exclude='*_test.go' -e 'http\.Client{' -e 'http\.Transport{' cmd internal | grep -v -x -e 'internal/provider/transport.go' | no_match -e .
CMD
expect_fail "negative: a file outside search.go, its tests and providertest changed (or a protected path)" <<'CMD'
out="$(git status --porcelain --untracked-files=all)" || exit 125
printf '%s\n' "$out" | sed -e 's/^...//' -e 's/^.* -> //' | grep -v -x -e '' -e 'versions/v6/Tasks.md' | grep -v -E -x -e 'internal/provider/search\.go' -e 'internal/provider/[a-z0-9_]+_test\.go' -e 'internal/provider/providertest/[a-z0-9_]+\.go' | no_match -e .
CMD
expect_ok "go.mod and go.sum unchanged" <<'CMD'
git diff --quiet ede3886 -- go.mod go.sum
CMD
expect_ok "make check" <<'CMD'
make check
CMD
finish
