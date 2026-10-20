//go:build ignore

// Supervisor probe for T201 (Redactor). Workers never edit it.
// Exit codes: 0 = OK/ACCEPTED, 1 = FAIL/REJECTED, 125 = setup error or crash.
package main

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"yipyip/internal/config"
)

func setup(f string, a ...any)    { fmt.Printf("SETUP: "+f+"\n", a...); os.Exit(125) }
func pass(f string, a ...any)     { fmt.Printf("OK: "+f+"\n", a...); os.Exit(0) }
func fail(f string, a ...any)     { fmt.Printf("FAIL: "+f+"\n", a...); os.Exit(1) }
func rejected(f string, a ...any) { fmt.Printf("REJECTED: "+f+"\n", a...); os.Exit(1) }
func accepted(f string, a ...any) { fmt.Printf("ACCEPTED: "+f+"\n", a...); os.Exit(0) }

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
	case "basic":
		if config.Redacted != "[redacted]" {
			fail("Redacted = %q", config.Redacted)
		}
		r := config.NewRedactor("SECRET123")
		if got := r.Redact("key=SECRET123 and SECRET123"); got != "key=[redacted] and [redacted]" {
			fail("Redact = %q", got)
		}
		var n *config.Redactor
		if got := n.Redact("plain"); got != "plain" {
			fail("nil Redactor changed text: %q", got)
		}
		r.Add("")
		if got := r.Redact("abc"); got != "abc" {
			fail("empty secret was registered: %q", got)
		}
		pass("basic redaction")
	case "leak":
		r := config.NewRedactor("SECRET123")
		r.Add("TOKEN-XYZ")
		inputs := []string{"SECRET123SECRET123", "a\"SECRET123\"b", "Authorization: Bearer SECRET123", "xTOKEN-XYZy"}
		var wg sync.WaitGroup
		var mu sync.Mutex
		var leaks []string
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for _, in := range inputs {
					out := r.Redact(in)
					if strings.Contains(out, "SECRET123") || strings.Contains(out, "TOKEN-XYZ") || !strings.Contains(out, "[redacted]") {
						mu.Lock()
						leaks = append(leaks, out)
						mu.Unlock()
					}
				}
			}()
		}
		wg.Wait()
		if len(leaks) > 0 {
			accepted("secret survived redaction: %q", leaks[0])
		}
		rejected("no secret survived redaction")
	case "overlap":
		r := config.NewRedactor("abc", "abcdef")
		if got := r.Redact("x abcdef y"); got != "x [redacted] y" {
			accepted("overlapping secrets left a fragment: %q", got)
		}
		rejected("longest secret replaced first")
	default:
		setup("unknown case %q", os.Args[1])
	}
}
