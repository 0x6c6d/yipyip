package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"
)

func parse(args ...string) (flags, int, bool, string) {
	var o, e bytes.Buffer
	f, c, d := parseArgs(args, &o, &e)
	return f, c, d, e.String()
}

func TestParseHeadlessFlags(t *testing.T) {
	f, code, done, _ := parse("-p", "hi", "--json", "--answer", "a", "--answer=b", "--approve", "bash, glob", "--approve=read_file,",
		"--dir", "/x", "--model=m", "--effort", "high", "--mode", "manual", "--timeout", "1m30s",
		"--max-model-calls=2", "--system-append", "-x", "--no-save", "--transcript", "t.txt", "--resume", "abc123abc123")
	if done || code != 0 || !f.headless {
		t.Fatalf("done=%v code=%d headless=%v", done, code, f.headless)
	}
	want := headlessFlags{prompt: "hi", json: true, answers: []string{"a", "b"}, approve: []string{"bash", "glob", "read_file"},
		dir: "/x", model: "m", effort: "high", mode: "manual", timeout: 90 * time.Second, maxCalls: 2,
		systemAppend: "-x", noSave: true, transcript: "t.txt"}
	if !reflect.DeepEqual(f.h, want) {
		t.Fatalf("got %+v", f.h)
	}
	if !f.resume || f.resumeID != "abc123abc123" {
		t.Fatalf("resume %v %q", f.resume, f.resumeID)
	}
	f, _, _, _ = parse("-p", "-")
	if f.h.prompt != "-" {
		t.Fatalf("prompt %q", f.h.prompt)
	}
	f, _, _, _ = parse("--prompt=x", "--answer", "-x")
	if f.h.prompt != "x" || !reflect.DeepEqual(f.h.answers, []string{"-x"}) {
		t.Fatalf("got %+v", f.h)
	}
	f, _, _, _ = parse("-p=y", "--resume", "-p", "z")
	_ = f
}

func TestParseHeadlessWithoutPrompt(t *testing.T) {
	for _, a := range [][]string{{"--json"}, {"--answer", "a"}, {"--approve", "bash"}, {"--dir", "x"}, {"--model", "m"},
		{"--effort", "e"}, {"--mode", "auto"}, {"--timeout", "1s"}, {"--max-model-calls", "1"},
		{"--system-append", "s"}, {"--no-save"}, {"--transcript", "t"}} {
		_, code, done, stderr := parse(a...)
		if code != 2 || !done || !strings.Contains(stderr, "-p") || !strings.Contains(stderr, a[0]) {
			t.Errorf("%v: code=%d done=%v stderr=%q", a, code, done, stderr)
		}
	}
}

func TestParseHeadlessBadValues(t *testing.T) {
	for _, a := range [][]string{{"-p"}, {"-p", "hi", "--answer"}, {"-p", ""}, {"-p", "hi", "--timeout", "x"},
		{"-p", "hi", "--timeout", "0s"}, {"-p", "hi", "--max-model-calls", "0"}, {"-p", "hi", "--max-model-calls", "abc"},
		{"-p", "hi", "--mode", "bogus"}, {"-p", "hi", "--resume"}} {
		_, code, done, stderr := parse(a...)
		if code != 2 || !done || !strings.HasPrefix(stderr, "yip: ") || !strings.Contains(stderr, "usage:") {
			t.Errorf("%v: code=%d done=%v stderr=%q", a, code, done, stderr)
		}
	}
}

func TestParseInteractiveUnchanged(t *testing.T) {
	for _, c := range []struct {
		args   []string
		resume bool
		id     string
	}{{nil, false, ""}, {[]string{"--resume"}, true, ""}, {[]string{"--resume", "abc"}, true, "abc"}, {[]string{"-c"}, true, ""}} {
		f, code, done, _ := parse(c.args...)
		if code != 0 || done || f.headless || f.resume != c.resume || f.resumeID != c.id {
			t.Errorf("%v: %+v code=%d done=%v", c.args, f, code, done)
		}
	}
}
