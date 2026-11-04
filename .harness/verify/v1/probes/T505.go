//go:build ignore

// Supervisor probe for T505 (approval prompt in the TUI; no unprompted bash). Workers never edit it.
// Exit codes: 0 = OK/ACCEPTED, 1 = FAIL/REJECTED, 125 = setup error or crash.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/provider/providertest"
	"yipyip/internal/tui"
)

func setup(f string, a ...any)    { fmt.Printf("SETUP: "+f+"\n", a...); os.Exit(125) }
func pass(f string, a ...any)     { fmt.Printf("OK: "+f+"\n", a...); os.Exit(0) }
func fail(f string, a ...any)     { fmt.Printf("FAIL: "+f+"\n", a...); os.Exit(1) }
func rejected(f string, a ...any) { fmt.Printf("REJECTED: "+f+"\n", a...); os.Exit(1) }
func accepted(f string, a ...any) { fmt.Printf("ACCEPTED: "+f+"\n", a...); os.Exit(0) }

type R = providertest.Response
type TC = providertest.ToolCall

func update(m tui.Model, msg tea.Msg) tui.Model {
	mm, _ := m.Update(msg)
	return mm.(tui.Model)
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("PANIC:", r)
			os.Exit(125)
		}
	}()
	if len(os.Args) != 2 || (os.Args[1] != "deny" && os.Args[1] != "yes") {
		setup("usage: probe deny|yes")
	}
	srv := providertest.New(R{ToolCalls: []TC{{ID: "c1", Name: "bash", Arguments: `{"command":"touch marker"}`}}}, R{Text: "finished"})
	defer srv.Close()
	base, err := os.MkdirTemp("", "tuiprobe")
	if err != nil {
		setup("%v", err)
	}
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
	m := tui.New(tui.Options{StartDir: dir, Session: s})
	m = update(m, tea.WindowSizeMsg{Width: 160, Height: 50})
	m, _ = m.Command("please touch the marker")
	prompted := false
	for i := 0; i < 100 && !prompted; i++ {
		time.Sleep(50 * time.Millisecond)
		m = m.Drain()
		v := m.View()
		prompted = strings.Contains(v, "y / n / a") && strings.Contains(v, "bash")
	}
	marker := filepath.Join(dir, "marker")
	if !prompted {
		if exists(marker) {
			accepted("bash ran in manual mode without a prompt")
		}
		fail("no approval prompt shown in manual mode")
	}
	time.Sleep(time.Second)
	if exists(marker) {
		accepted("bash ran before the user answered")
	}
	key := "n"
	if os.Args[1] == "yes" {
		key = "y"
	}
	m = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	done := make(chan struct{})
	go func() { s.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		fail("turn did not finish after answering")
	}
	m = m.Drain()
	if os.Args[1] == "yes" {
		if !exists(marker) || strings.Contains(m.View(), "y / n / a") {
			fail("'y' must run the command and close the prompt")
		}
		pass("approved bash ran")
	}
	denied := false
	if reqs := srv.Requests(); len(reqs) == 2 {
		for _, msg := range reqs[1].Messages {
			if msg.Role == "tool" && strings.Contains(msg.Content, "denied by user") {
				denied = true
			}
		}
	}
	if exists(marker) || !denied {
		accepted("after 'n': marker exists=%v, model told 'denied by user'=%v", exists(marker), denied)
	}
	rejected("denied bash did not run")
}
