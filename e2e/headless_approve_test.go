package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yipyip/internal/provider/providertest"
)

// approveServer returns a fake that calls bash (writing ran.txt) and then
// answers ok.
func approveServer() *providertest.Server {
	return providertest.New(
		providertest.Response{ToolCalls: []providertest.ToolCall{{ID: "c1", Name: "bash", Arguments: `{"command":"echo hi > ran.txt"}`}}},
		providertest.Response{Text: "ok"},
	)
}

func approvalRecords(recs []map[string]any) []map[string]any {
	var out []map[string]any
	for _, r := range recs {
		if r["type"] == "approval" {
			out = append(out, r)
		}
	}
	return out
}

func ranExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "ran.txt"))
	return err == nil
}

// runApprove runs one headless turn against a fresh fake and checks the single
// approval record for bash with the wanted decision.
func runApprove(t *testing.T, decision string, args ...string) {
	t.Helper()
	srv := approveServer()
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, append([]string{"-p", "go", "--json"}, args...)...)
	if res.code != 0 {
		t.Fatalf("%v: code %d stdout %q stderr %q", args, res.code, res.stdout, res.stderr)
	}
	recs := jsonLines(t, res.stdout)
	aps := approvalRecords(recs)
	if len(aps) != 1 || aps[0]["tool"] != "bash" || aps[0]["decision"] != decision {
		t.Fatalf("%v: approval records = %v", args, aps)
	}
	if aps[0]["agent"] != "main" || aps[0]["summary"] != "echo hi > ran.txt" {
		t.Errorf("%v: approval record = %v", args, aps[0])
	}
	if got, want := ranExists(dir), decision == "approved"; got != want {
		t.Errorf("%v: ran.txt exists = %v, want %v", args, got, want)
	}
	if n := len(srv.Requests()); n != 2 {
		t.Fatalf("%v: requests = %d, want 2", args, n)
	}
	msg := lastToolMessage(t, srv, 1)
	if decision == "denied" && !strings.Contains(msg, "denied") {
		t.Errorf("%v: tool message = %q, want a denial", args, msg)
	}
	if decision == "approved" && strings.Contains(msg, "denied") {
		t.Errorf("%v: tool message = %q, want no denial", args, msg)
	}
	if r := lastResult(t, recs); r["final_text"] != "ok" {
		t.Errorf("%v: result = %v", args, r)
	}
	checkNoSecret(t, res)
}

func TestHeadlessApprove(t *testing.T) {
	runApprove(t, "denied", "--mode", "manual")
	runApprove(t, "approved", "--mode", "manual", "--approve", "bash")
	runApprove(t, "approved", "--mode", "manual", "--approve", "all")

	// Without --json the decision goes to stderr.
	srv := approveServer()
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--mode", "manual")
	if res.code != 0 || !strings.Contains(res.stderr, "approval bash denied") {
		t.Errorf("plain: code %d stderr %q", res.code, res.stderr)
	}
	if ranExists(dir) {
		t.Errorf("plain: ran.txt exists after a denial")
	}
}

func TestHeadlessApproveUnknown(t *testing.T) {
	srv := approveServer()
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	for _, mode := range []string{"manual", "auto"} {
		res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--mode", mode, "--approve", "bash,nosuchtool")
		if res.code != 2 {
			t.Errorf("%s: code %d, want 2", mode, res.code)
		}
		if !strings.Contains(res.stderr, "yip: unknown tool nosuchtool in --approve") {
			t.Errorf("%s: stderr %q", mode, res.stderr)
		}
		checkNoSecret(t, res)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("requests = %d, want 0", n)
	}
	if ranExists(dir) {
		t.Errorf("ran.txt exists")
	}
}

func TestHeadlessAutoNoApproval(t *testing.T) {
	srv := approveServer()
	defer srv.Close()
	home, dir := headlessDirs(t, srv)
	res := runYip(t, home, dir, "", headlessEnv, "-p", "go", "--json")
	if res.code != 0 {
		t.Fatalf("code %d stdout %q stderr %q", res.code, res.stdout, res.stderr)
	}
	if !ranExists(dir) {
		t.Errorf("ran.txt missing in auto mode")
	}
	if aps := approvalRecords(jsonLines(t, res.stdout)); len(aps) != 0 {
		t.Errorf("approval records = %v, want none", aps)
	}
	checkNoSecret(t, res)
}

func TestHeadlessManualOtherToolDenied(t *testing.T) {
	runApprove(t, "denied", "--mode", "manual", "--approve", "write_file")
}
