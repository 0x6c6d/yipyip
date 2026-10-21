package agent

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"yipyip/internal/provider/providertest"
)

// undoSession returns an auto-mode session talking to a fake provider that
// plays resps.
func undoSession(t *testing.T, resps ...fakeResp) (*Session, string) {
	t.Helper()
	srv := providertest.New(resps...)
	t.Cleanup(srv.Close)
	s, dir := newLoopSession(t, srv)
	s.SetMode(ModeAuto)
	return s, dir
}

func writeCall(id, path, content string) fakeCall {
	return fakeCall{ID: id, Name: "write_file", Arguments: `{"path":"` + path + `","content":"` + content + `"}`}
}

func readOrMissing(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func mustUndo(t *testing.T, s *Session) UndoResult {
	t.Helper()
	res, err := s.Undo()
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	return res
}

func TestUndoRestoresModified(t *testing.T) {
	s, dir := undoSession(t,
		fakeResp{ToolCalls: []fakeCall{
			writeCall("c1", "a.txt", "new"),
			{ID: "c2", Name: "edit_file", Arguments: `{"path":"a.txt","old_string":"new","new_string":"newer"}`}}},
		fakeResp{Text: "done"})
	a := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(a, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	submitAndWait(t, s, "change it")
	if got := readOrMissing(a); got != "newer" {
		t.Fatalf("tools did not run: %q", got)
	}
	res := mustUndo(t, s)
	if got := readOrMissing(a); got != "original" {
		t.Fatalf("after undo a.txt = %q", got)
	}
	if !slices.Equal(res.Restored, []string{"a.txt"}) || len(res.Deleted) != 0 || len(res.BashNotReverted) != 0 {
		t.Fatalf("UndoResult = %+v", res)
	}
}

func TestUndoDeletesCreated(t *testing.T) {
	s, dir := undoSession(t,
		fakeResp{ToolCalls: []fakeCall{writeCall("c1", "sub/dir/new.txt", "x"), writeCall("c2", "top.txt", "y")}},
		fakeResp{Text: "done"})
	submitAndWait(t, s, "create")
	res := mustUndo(t, s)
	for _, p := range []string{"sub/dir/new.txt", "top.txt"} {
		if _, err := os.Lstat(filepath.Join(dir, p)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still exists: %v", p, err)
		}
	}
	if fi, err := os.Stat(filepath.Join(dir, "sub", "dir")); err != nil || !fi.IsDir() {
		t.Fatalf("created directories must be left: %v", err)
	}
	if !slices.Equal(res.Deleted, []string{"sub/dir/new.txt", "top.txt"}) || len(res.Restored) != 0 {
		t.Fatalf("UndoResult = %+v", res)
	}
}

func TestUndoRemovesTurn(t *testing.T) {
	s, _ := undoSession(t,
		fakeResp{Text: "first answer"},
		fakeResp{ToolCalls: []fakeCall{writeCall("c1", "f.txt", "z")}},
		fakeResp{Text: "second answer"})
	submitAndWait(t, s, "one")
	before := s.History()
	tok := s.Tokens()
	submitAndWait(t, s, "two")
	if len(s.History()) <= len(before) {
		t.Fatal("second turn did not extend the history")
	}
	tok2 := s.Tokens()
	mustUndo(t, s)
	h := s.History()
	if len(h) != len(before) || h[len(h)-1].Content != "first answer" {
		t.Fatalf("history after undo = %+v, want %d messages", h, len(before))
	}
	if s.Tokens() != tok2 || tok2 == tok {
		t.Fatalf("tokens changed by undo: %+v -> %+v", tok2, s.Tokens())
	}
	mustUndo(t, s)
	if h := s.History(); len(h) != 1 || h[0].Role != "system" {
		t.Fatalf("history after second undo = %+v", h)
	}
}

func TestUndoTwice(t *testing.T) {
	s, dir := undoSession(t,
		fakeResp{ToolCalls: []fakeCall{writeCall("c1", "a.txt", "one")}},
		fakeResp{Text: "t1"},
		fakeResp{ToolCalls: []fakeCall{writeCall("c2", "a.txt", "two")}},
		fakeResp{Text: "t2"})
	a := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(a, []byte("orig"), 0o644); err != nil {
		t.Fatal(err)
	}
	submitAndWait(t, s, "first")
	submitAndWait(t, s, "second")
	mustUndo(t, s)
	if got := readOrMissing(a); got != "one" {
		t.Fatalf("after first undo %q", got)
	}
	mustUndo(t, s)
	if got := readOrMissing(a); got != "orig" {
		t.Fatalf("after second undo %q", got)
	}
	if _, err := s.Undo(); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("third undo: %v", err)
	}
}

func TestUndoListsBash(t *testing.T) {
	s, dir := undoSession(t,
		fakeResp{ToolCalls: []fakeCall{
			{ID: "b1", Name: "bash", Arguments: `{"command":"echo one > b.txt"}`},
			{ID: "b2", Name: "bash", Arguments: `{"command":"echo two"}`}}},
		fakeResp{Text: "done"})
	submitAndWait(t, s, "run")
	res := mustUndo(t, s)
	if !slices.Equal(res.BashNotReverted, []string{"echo one > b.txt", "echo two"}) {
		t.Fatalf("BashNotReverted = %v", res.BashNotReverted)
	}
	// Bash effects are not reverted.
	if got := readOrMissing(filepath.Join(dir, "b.txt")); got != "one\n" {
		t.Fatalf("b.txt = %q", got)
	}
}

func TestUndoSubagentWrites(t *testing.T) {
	srv := providertest.NewFunc(func(n int, req providertest.Request) fakeResp {
		if isSub(req) {
			if countTools(req) == 0 {
				return fakeResp{ToolCalls: []fakeCall{writeCall("sw", "s.txt", "by-sub")}}
			}
			return fakeResp{Text: "sub done"}
		}
		return spawnWaitMain("write s.txt", req)
	})
	defer srv.Close()
	s := newSubSession(t, srv, "")
	s.SetMode(ModeAuto)
	p := filepath.Join(s.StartDir(), "s.txt")
	if err := os.WriteFile(p, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	submitAndWait(t, s, mainPrompt)
	if got := readOrMissing(p); got != "by-sub" {
		t.Fatalf("subagent did not write: %q", got)
	}
	res := mustUndo(t, s)
	if got := readOrMissing(p); got != "before" {
		t.Fatalf("after undo s.txt = %q", got)
	}
	if !slices.Equal(res.Restored, []string{"s.txt"}) {
		t.Fatalf("UndoResult = %+v", res)
	}
}

func TestUndoNothing(t *testing.T) {
	s, _ := undoSession(t, fakeResp{ToolCalls: []fakeCall{writeCall("c1", "a.txt", "x")}}, fakeResp{Text: "ok"})
	if _, err := s.Undo(); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("Undo on a fresh session: %v", err)
	}
	submitAndWait(t, s, "write")
	s.Reset()
	if _, err := s.Undo(); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("Undo after Reset: %v", err)
	}
	if got := readOrMissing(filepath.Join(s.StartDir(), "a.txt")); got != "x" {
		t.Fatalf("Reset must not touch files: %q", got)
	}
}

func TestUndoBusy(t *testing.T) {
	s, _ := undoSession(t, fakeResp{Text: "slow", Delay: 5 * time.Second})
	if err := s.Submit("x"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Undo(); !errors.Is(err, ErrBusy) {
		t.Fatalf("Undo during a turn: %v", err)
	}
	s.Cancel()
	waitTurn(t, s, 5*time.Second)
	// A cancelled turn still has a record and can be undone.
	mustUndo(t, s)
	if h := s.History(); len(h) != 1 {
		t.Fatalf("history after undo of the cancelled turn = %+v", h)
	}
}

func TestUndoRestoresMode(t *testing.T) {
	s, dir := undoSession(t,
		fakeResp{ToolCalls: []fakeCall{
			writeCall("c1", "m.txt", "changed"),
			{ID: "b1", Name: "bash", Arguments: `{"command":"chmod 644 m.txt"}`}}},
		fakeResp{Text: "done"})
	p := filepath.Join(dir, "m.txt")
	if err := os.WriteFile(p, []byte("orig"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	submitAndWait(t, s, "change")
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("setup: mode not changed: %v", err)
	}
	mustUndo(t, s)
	fi, err := os.Stat(p)
	if err != nil || fi.Mode().Perm() != 0o600 || readOrMissing(p) != "orig" {
		t.Fatalf("after undo: err=%v mode=%v content=%q", err, fi.Mode(), readOrMissing(p))
	}
}
