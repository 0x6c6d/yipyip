package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// headlessFlags are the options of a headless run (-p/--prompt).
type headlessFlags struct {
	prompt              string        // value of -p/--prompt ("-" = read stdin later)
	json                bool          // --json
	answers             []string      // --answer, repeatable, in order
	approve             []string      // --approve, repeatable; split on ",", trimmed, empty parts dropped
	dir                 string        // --dir
	model, effort, mode string        // --model, --effort, --mode
	timeout             time.Duration // --timeout (0 = none)
	maxCalls            int           // --max-model-calls (0 = unlimited)
	systemAppend        string        // --system-append
	noSave              bool          // --no-save
	transcript          string        // --transcript
}

// headlessValueFlags are the headless flags that take a value.
var headlessValueFlags = map[string]bool{
	"-p": true, "--prompt": true, "--answer": true, "--approve": true, "--dir": true,
	"--model": true, "--effort": true, "--mode": true, "--timeout": true,
	"--max-model-calls": true, "--system-append": true, "--transcript": true,
}

// headlessBoolFlags are the headless flags without a value.
var headlessBoolFlags = map[string]bool{"--json": true, "--no-save": true}

// setHeadlessValue stores the value of the value flag name in f.
func (f *flags) setHeadlessValue(name, val string) error {
	h := &f.h
	switch name {
	case "-p", "--prompt":
		if val == "" {
			return fmt.Errorf("empty prompt")
		}
		h.prompt, f.headless = val, true
	case "--answer":
		h.answers = append(h.answers, val)
	case "--approve":
		for _, p := range strings.Split(val, ",") {
			if p = strings.TrimSpace(p); p != "" {
				h.approve = append(h.approve, p)
			}
		}
	case "--dir":
		h.dir = val
	case "--model":
		h.model = val
	case "--effort":
		h.effort = val
	case "--mode":
		if val != "auto" && val != "manual" {
			return fmt.Errorf("--mode must be auto or manual")
		}
		h.mode = val
	case "--timeout":
		d, err := time.ParseDuration(val)
		if err != nil || d <= 0 {
			return fmt.Errorf("--timeout needs a positive duration like 90s")
		}
		h.timeout = d
	case "--max-model-calls":
		n, err := strconv.Atoi(val)
		if err != nil || n < 1 {
			return fmt.Errorf("--max-model-calls needs an integer >= 1")
		}
		h.maxCalls = n
	case "--system-append":
		h.systemAppend = val
	case "--transcript":
		h.transcript = val
	}
	return nil
}
