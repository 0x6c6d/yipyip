//go:build ignore

// Supervisor probe for T601 (undo journal). Workers never edit it.
// Exit codes: 0 = OK/ACCEPTED, 1 = FAIL/REJECTED, 125 = setup error or crash.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
)

func setup(f string, a ...any)    { fmt.Printf("SETUP: "+f+"\n", a...); os.Exit(125) }
func pass(f string, a ...any)     { fmt.Printf("OK: "+f+"\n", a...); os.Exit(0) }
func fail(f string, a ...any)     { fmt.Printf("FAIL: "+f+"\n", a...); os.Exit(1) }
func rejected(f string, a ...any) { fmt.Printf("REJECTED: "+f+"\n", a...); os.Exit(1) }
func accepted(f string, a ...any) { fmt.Printf("ACCEPTED: "+f+"\n", a...); os.Exit(0) }

type R = providertest.Response
type TC = providertest.ToolCall

func session(srv *providertest.Server) (*agent.Session, string) {
	base, err := os.MkdirTemp("", "undoprobe")
	if err != nil {
		setup("%v", err)
	}
	base, _ = filepath.EvalSymlinks(base)
	home, dir := filepath.Join(base, "home"), filepath.Join(base, "start")
	os.MkdirAll(filepath.Join(home, ".config", "yipyip"), 0o755)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(home, ".config", "yipyip", "config.json"), []byte(providertest.ConfigJSON(srv.URL, "{env:FAKE_KEY}", 8000)), 0o644)
	cfg, err := config.Load(home, dir, func(k string) string {
		if k == "FAKE_KEY" {
			return "SECRET123"
		}
		return ""
	})
	if err != nil {
		setup("config.Load: %v", err)
	}
	s, err := agent.NewSession(agent.Options{Config: cfg, StartDir: dir})
	if err != nil {
		setup("NewSession: %v", err)
	}
	s.SetMode(agent.ModeAuto)
	return s, dir
}

func turn(s *agent.Session, text string) {
	if err := s.Submit(text); err != nil {
		setup("Submit: %v", err)
	}
	done := make(chan struct{})
	go func() { s.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		setup("turn did not finish")
	}
}

func read(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("PANIC:", r)
			os.Exit(125)
		}
	}()
	if len(os.Args) != 2 {
		setup("usage: probe <case>")
	}
	switch os.Args[1] {
	case "nothing":
		srv := providertest.New()
		defer srv.Close()
		s, _ := session(srv)
		if _, err := s.Undo(); err == nil || !errors.Is(err, agent.ErrNothingToUndo) {
			accepted("Undo on a fresh session: %v", err)
		}
		rejected("nothing to undo")
	case "busy":
		srv := providertest.New(R{Text: "slow", Delay: 3 * time.Second})
		defer srv.Close()
		s, _ := session(srv)
		if err := s.Submit("x"); err != nil {
			setup("Submit: %v", err)
		}
		_, err := s.Undo()
		s.Cancel()
		if err == nil || !errors.Is(err, agent.ErrBusy) {
			accepted("Undo during a running turn: %v", err)
		}
		rejected("undo refused while busy")
	case "restore":
		srv := providertest.New(
			R{ToolCalls: []TC{{ID: "c1", Name: "write_file", Arguments: `{"path":"a.txt","content":"turn1"}`}}},
			R{Text: "t1"},
			R{ToolCalls: []TC{
				{ID: "c2", Name: "write_file", Arguments: `{"path":"a.txt","content":"turn2"}`},
				{ID: "c3", Name: "edit_file", Arguments: `{"path":"a.txt","old_string":"turn2","new_string":"turn2b"}`},
				{ID: "c4", Name: "write_file", Arguments: `{"path":"b/new.txt","content":"created"}`},
				{ID: "c5", Name: "bash", Arguments: `{"command":"echo hi > c.txt"}`}}},
			R{Text: "t2"})
		defer srv.Close()
		s, dir := session(srv)
		a := filepath.Join(dir, "a.txt")
		os.WriteFile(a, []byte("original"), 0o600)
		turn(s, "first")
		h1 := len(s.History())
		turn(s, "second")
		if read(a) != "turn2b" || read(filepath.Join(dir, "b", "new.txt")) != "created" {
			setup("tools did not run: a=%q", read(a))
		}
		res, err := s.Undo()
		if err != nil {
			fail("Undo: %v", err)
		}
		if read(a) != "turn1" || read(filepath.Join(dir, "b", "new.txt")) != "<missing>" {
			fail("after undo: a=%q b/new.txt=%q", read(a), read(filepath.Join(dir, "b", "new.txt")))
		}
		if !slices.Contains(res.Restored, "a.txt") || !slices.Contains(res.Deleted, "b/new.txt") || !slices.Contains(res.BashNotReverted, "echo hi > c.txt") {
			fail("UndoResult = %+v", res)
		}
		if len(s.History()) != h1 {
			fail("history after undo has %d messages, want %d", len(s.History()), h1)
		}
		if _, err := s.Undo(); err != nil || read(a) != "original" || len(s.History()) != 1 {
			fail("second undo: err=%v a=%q history=%d", err, read(a), len(s.History()))
		}
		if fi, err := os.Stat(a); err != nil || fi.Mode().Perm() != 0o600 {
			fail("file mode not restored: %v", fi.Mode())
		}
		if _, err := s.Undo(); !errors.Is(err, agent.ErrNothingToUndo) {
			fail("third undo: %v", err)
		}
		for _, ev := range s.PollEvents() {
			if strings.Contains(ev.Text, "SECRET123") {
				fail("secret in events")
			}
		}
		pass("undo restores, deletes, removes turns, twice")
	default:
		setup("unknown case %q", os.Args[1])
	}
}
