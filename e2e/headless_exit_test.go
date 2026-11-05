package e2e

import (
	"strings"
	"testing"

	"yipyip/internal/provider/providertest"
)

func TestHeadlessExit0(t *testing.T) {
	srv := providertest.New(providertest.Response{Text: "hello"})
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "hi", "--json")
	if res.code != 0 {
		t.Fatalf("code %d stderr %q, want 0", res.code, res.stderr)
	}
	r := lastResult(t, jsonLines(t, res.stdout))
	if r["exit_code"] != float64(0) || r["reason"] != "done" {
		t.Errorf("result %v, want exit_code 0 reason done", r)
	}
	checkNoSecret(t, res)
}

func TestHeadlessExit1(t *testing.T) {
	t.Run("missing env", func(t *testing.T) {
		srv := providertest.New()
		defer srv.Close()
		home, dir := cliDirs(t)
		writeCLIGlobalConfig(t, home, providertest.ConfigJSON(srv.URL, "{env:MISSING_KEY}", 100000))
		res := runYip(t, home, dir, "", nil, "-p", "hi")
		if res.code != 1 || !strings.Contains(res.stderr, "MISSING_KEY") {
			t.Errorf("code %d stderr %q, want 1 naming MISSING_KEY", res.code, res.stderr)
		}
	})
	t.Run("provider 400", func(t *testing.T) {
		srv := providertest.New(providertest.Response{Status: 400, Body: `{"error":"bad request"}`})
		defer srv.Close()
		home, dir := headlessDirs(t, srv)
		res := runYip(t, home, dir, "", headlessEnv, "-p", "hi", "--json")
		if res.code != 1 {
			t.Fatalf("code %d stderr %q, want 1", res.code, res.stderr)
		}
		recs := jsonLines(t, res.stdout)
		r := lastResult(t, recs)
		if r["exit_code"] != float64(1) || r["reason"] != "error" {
			t.Errorf("result %v, want exit_code 1 reason error", r)
		}
		if typesOf(recs)["error"] == 0 {
			t.Errorf("no error record in %q", res.stdout)
		}
		checkNoSecret(t, res)
	})
}

func TestHeadlessExit2(t *testing.T) {
	srv := providertest.New()
	defer srv.Close()
	home, dir := headlessDirs(t, srv)

	if res := runYip(t, home, dir, "", headlessEnv, "--json"); res.code != 2 || res.stdout != "" {
		t.Errorf("--json without -p: code %d stdout %q, want 2 and empty stdout", res.code, res.stdout)
	}
	for _, args := range [][]string{
		{"-p", "x", "--timeout", "bogus", "--json"},
		{"-p", "x", "--json", "--bogus"},
	} {
		res := runYip(t, home, dir, "", headlessEnv, args...)
		if res.code != 2 {
			t.Errorf("%v: code %d, want 2", args, res.code)
		}
		lines := strings.Split(strings.TrimRight(res.stdout, "\n"), "\n")
		if len(lines) != 1 {
			t.Errorf("%v: stdout %q, want exactly one line", args, res.stdout)
			continue
		}
		r := lastResult(t, jsonLines(t, res.stdout))
		if r["exit_code"] != float64(2) || r["reason"] != "usage" {
			t.Errorf("%v: result %v, want exit_code 2 reason usage", args, r)
		}
		if res.stderr == "" {
			t.Errorf("%v: empty stderr", args)
		}
		checkNoSecret(t, res)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("usage errors reached the provider %d times", n)
	}
}
