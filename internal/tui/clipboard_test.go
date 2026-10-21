package tui

import (
	"bytes"
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	osc52 "github.com/aymanbagabas/go-osc52/v2"
)

// countingWriter records each Write call separately.
type countingWriter struct{ writes []string }

func (c *countingWriter) Write(p []byte) (int, error) {
	c.writes = append(c.writes, string(p))
	return len(p), nil
}

func TestCopyOSC52Sequence(t *testing.T) {
	var buf bytes.Buffer
	if err := writeClipboard(&buf, false, "hello"); err != nil {
		t.Fatalf("writeClipboard: %v", err)
	}
	want := osc52.New("hello").String()
	if buf.String() != want {
		t.Fatalf("got %q, want %q", buf.String(), want)
	}
	if !strings.Contains(buf.String(), "]52;c;aGVsbG8=") {
		t.Fatalf("sequence %q lacks the OSC 52 payload", buf.String())
	}

	var cw countingWriter
	_ = writeClipboard(&cw, false, "hello")
	if len(cw.writes) != 1 || cw.writes[0] != want {
		t.Fatalf("want one Write of %q, got %q", want, cw.writes)
	}
}

func TestCopyOSC52Unicode(t *testing.T) {
	const in = "héllo ✻"
	var buf bytes.Buffer
	if err := writeClipboard(&buf, false, in); err != nil {
		t.Fatalf("writeClipboard: %v", err)
	}
	s := buf.String()
	const prefix = "]52;c;"
	i := strings.Index(s, prefix)
	if i < 0 {
		t.Fatalf("no OSC 52 prefix in %q", s)
	}
	payload := s[i+len(prefix):]
	if j := strings.IndexAny(payload, "\a\x1b"); j >= 0 {
		payload = payload[:j]
	}
	got, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("decode %q: %v", payload, err)
	}
	if string(got) != in {
		t.Fatalf("round-trip: got %q, want %q", got, in)
	}
}

func TestCopyClipboardOption(t *testing.T) {
	var gotText []string
	fake := func(s string) error {
		gotText = append(gotText, s)
		return nil
	}
	m := New(Options{Clipboard: fake})
	if m.clip == nil {
		t.Fatal("m.clip is nil")
	}
	if err := m.clip("x"); err != nil {
		t.Fatalf("clip: %v", err)
	}
	if len(gotText) != 1 || gotText[0] != "x" {
		t.Fatalf("fake called with %q, want [x]", gotText)
	}

	d := New(Options{})
	if d.clip == nil {
		t.Fatal("default m.clip is nil")
	}
	if reflect.ValueOf(d.clip).Pointer() != reflect.ValueOf(defaultClipboard).Pointer() {
		t.Fatal("default model does not use defaultClipboard")
	}
}
