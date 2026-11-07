//go:build ignore

// Supervisor probe for v13 T101 (workers never edit it). Usage: probe <case>
//
//	xdg      GlobalDir is <home>/.config/yipyip although XDG_CONFIG_HOME is set (exit 0)
//	nohome   home "" leaves GlobalDir empty (exit 0)
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"yipyip/internal/config"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Println("usage: probe xdg|nohome")
		os.Exit(125)
	}
	switch os.Args[1] {
	case "xdg":
		home, err := os.MkdirTemp("", "v13home")
		if err != nil {
			fmt.Println(err)
			os.Exit(125)
		}
		defer os.RemoveAll(home)
		start, _ := os.MkdirTemp("", "v13start")
		defer os.RemoveAll(start)
		cfg, err := config.Load(home, start, func(k string) string {
			if k == "XDG_CONFIG_HOME" {
				return "/elsewhere"
			}
			return ""
		})
		if err != nil {
			fmt.Println("Load:", err)
			os.Exit(125)
		}
		want := filepath.Join(home, ".config", "yipyip")
		if cfg.GlobalDir != want {
			fmt.Printf("GlobalDir = %q, want %q\n", cfg.GlobalDir, want)
			os.Exit(1)
		}
		if _, err := os.Stat(want); err == nil {
			fmt.Println("Load created the config dir")
			os.Exit(1)
		}
	case "nohome":
		start, _ := os.MkdirTemp("", "v13start")
		defer os.RemoveAll(start)
		cfg, err := config.Load("", start, func(string) string { return "" })
		if err != nil {
			// An error for an empty home is acceptable; nothing is looked up then.
			return
		}
		if cfg.GlobalDir != "" {
			fmt.Printf("GlobalDir = %q for empty home\n", cfg.GlobalDir)
			os.Exit(1)
		}
	default:
		os.Exit(125)
	}
}
