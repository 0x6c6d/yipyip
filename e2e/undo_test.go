package e2e

import (
	"strings"
	"testing"

	"yipyip/internal/provider/providertest"
)

type resp = providertest.Response

func call(id, name, args string) providertest.ToolCall {
	return providertest.ToolCall{ID: id, Name: name, Arguments: args}
}

func mustContain(t *testing.T, view string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(view, s) {
			t.Errorf("view does not contain %q:\n%s", s, view)
		}
	}
}

func mustNotContain(t *testing.T, view string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if strings.Contains(view, s) {
			t.Errorf("view contains %q:\n%s", s, view)
		}
	}
}

func TestUndoRestoresModified(t *testing.T) {
	e := newEnv(t,
		resp{ToolCalls: []providertest.ToolCall{
			call("c1", "write_file", `{"path":"a.txt","content":"new"}`),
			call("c2", "edit_file", `{"path":"a.txt","old_string":"new","new_string":"newer"}`)}},
		resp{Text: "done"})
	e.write(t, "a.txt", "original")
	e.submit(t, "change it")
	if got := e.read("a.txt"); got != "newer" {
		t.Fatalf("tools did not run: a.txt = %q", got)
	}
	e.submit(t, "/undo")
	if got := e.read("a.txt"); got != "original" {
		t.Fatalf("after /undo a.txt = %q", got)
	}
	v := e.Model.View()
	mustContain(t, v, "undone:", "restored a.txt")
	mustNotContain(t, v, "deleted ", "not reverted")
}

func TestUndoDeletesCreated(t *testing.T) {
	e := newEnv(t,
		resp{ToolCalls: []providertest.ToolCall{
			call("c1", "write_file", `{"path":"sub/new.txt","content":"x"}`),
			call("c2", "write_file", `{"path":"top.txt","content":"y"}`)}},
		resp{Text: "done"})
	e.submit(t, "create files")
	if e.read("sub/new.txt") != "x" || e.read("top.txt") != "y" {
		t.Fatal("tools did not run")
	}
	e.submit(t, "/undo")
	for _, p := range []string{"sub/new.txt", "top.txt"} {
		if got := e.read(p); got != "<missing>" {
			t.Errorf("%s still exists: %q", p, got)
		}
	}
	v := e.Model.View()
	mustContain(t, v, "undone:", "deleted sub/new.txt", "deleted top.txt")
}

func TestUndoRemovesTurn(t *testing.T) {
	e := newEnv(t,
		resp{Text: "first answer"},
		resp{ToolCalls: []providertest.ToolCall{call("c1", "write_file", `{"path":"b.txt","content":"zzz"}`)}},
		resp{Text: "second answer"},
		resp{Text: "third answer"})
	e.submit(t, "keep this message")
	e.submit(t, "drop this message")
	v := e.Model.View()
	mustContain(t, v, "> drop this message", "second answer", "write_file")

	e.submit(t, "/undo")
	v = e.Model.View()
	mustContain(t, v, "> keep this message", "first answer", "undone:", "deleted b.txt")
	mustNotContain(t, v, "drop this message", "second answer", "write_file")

	e.submit(t, "after undo")
	reqs := e.Server.Requests()
	last := reqs[len(reqs)-1]
	var users []string
	for _, msg := range last.Messages {
		if msg.Role == "user" {
			users = append(users, msg.Content)
		}
	}
	if len(users) != 2 || users[0] != "keep this message" || users[1] != "after undo" {
		t.Fatalf("user messages of the next request = %q", users)
	}
	if strings.Contains(string(last.Body), "drop this message") {
		t.Fatal("undone message still in the next request")
	}
}

func TestUndoTwice(t *testing.T) {
	e := newEnv(t,
		resp{ToolCalls: []providertest.ToolCall{call("c1", "write_file", `{"path":"one.txt","content":"1"}`)}},
		resp{Text: "done one"},
		resp{ToolCalls: []providertest.ToolCall{call("c2", "write_file", `{"path":"two.txt","content":"2"}`)}},
		resp{Text: "done two"})
	e.submit(t, "turn one")
	e.submit(t, "turn two")

	e.submit(t, "/undo")
	if e.read("two.txt") != "<missing>" || e.read("one.txt") != "1" {
		t.Fatalf("first undo: one=%q two=%q", e.read("one.txt"), e.read("two.txt"))
	}
	mustContain(t, e.Model.View(), "deleted two.txt", "> turn one")

	e.submit(t, "/undo")
	if e.read("one.txt") != "<missing>" {
		t.Fatalf("second undo: one.txt = %q", e.read("one.txt"))
	}
	v := e.Model.View()
	mustContain(t, v, "deleted one.txt")
	mustNotContain(t, v, "> turn one", "done one")

	// A third undo has nothing left and changes no file.
	e.write(t, "keep.txt", "k")
	e.submit(t, "/undo")
	mustContain(t, e.Model.View(), "nothing to undo")
	if e.read("keep.txt") != "k" {
		t.Fatal("third undo touched a file")
	}
}

func TestUndoListsBash(t *testing.T) {
	const cmd = "echo hi > c.txt"
	e := newEnv(t,
		resp{ToolCalls: []providertest.ToolCall{call("c1", "bash", `{"command":"echo hi > c.txt"}`)}},
		resp{Text: "done"})
	e.submit(t, "run it")
	if got := strings.TrimSpace(e.read("c.txt")); got != "hi" {
		t.Fatalf("bash did not run: c.txt = %q", got)
	}
	e.submit(t, "/undo")
	mustContain(t, e.Model.View(), "undone:", "not reverted (bash): "+cmd)
	if got := strings.TrimSpace(e.read("c.txt")); got != "hi" {
		t.Fatalf("bash effect was reverted: c.txt = %q", got)
	}
}
