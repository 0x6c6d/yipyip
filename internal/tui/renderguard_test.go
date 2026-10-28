package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/config"
	"yipyip/internal/crashlog"
)

// guardModel returns a model with a crash logger in a temp dir and three
// plain blocks; the log path is returned too.
func guardModel(t *testing.T, texts ...string) (*Model, string) {
	t.Helper()
	dir := t.TempDir()
	red := config.NewRedactor("SECRET123")
	crashlog.SetDefault(crashlog.New(crashlog.Options{
		Version: "test",
		Redact:  red.Redact,
		Resolve: func(rel string) (string, error) { return dir + "/" + rel, nil },
	}))
	t.Cleanup(func() { crashlog.SetDefault(nil); renderBlockHook = nil })
	mm := New(Options{StartDir: dir})
	for _, s := range texts {
		mm.add(blockInfo, s)
	}
	p, _ := crashlog.Default().Path()
	return &mm, p
}

func panicOn(text string) {
	renderBlockHook = func(b *block) {
		if strings.Contains(b.text, text) {
			panic("boom")
		}
	}
}

func TestRenderGuardBlock(t *testing.T) {
	m, _ := guardModel(t, "first-text", "middle-text", "third-text")
	panicOn("middle-text")
	var out string
	for _, b := range m.blocks {
		out += m.renderBlock(b, 80) + "\n"
	}
	out = ansi.Strip(out)
	for _, w := range []string{"first-text", "third-text", "[render error]"} {
		if !strings.Contains(out, w) {
			t.Fatalf("missing %q in %q", w, out)
		}
	}
	if strings.Contains(out, "middle-text") {
		t.Fatalf("panicking block text shown: %q", out)
	}
}

func TestRenderGuardLogged(t *testing.T) {
	m, path := guardModel(t, "a", "middle-text")
	panicOn("middle-text")
	m.renderBlock(m.blocks[1], 80)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "where: render block") {
		t.Fatalf("log lacks where: %s", data)
	}
	if strings.Contains(string(data), "middle-text") {
		t.Fatalf("log has block text: %s", data)
	}
}

func TestRenderGuardDoesNotLeakText(t *testing.T) {
	m, path := guardModel(t, "leaky SECRET123 text")
	renderBlockHook = func(b *block) { panic("value " + b.text) }
	out := ansi.Strip(m.renderBlock(m.blocks[0], 80))
	if out != "[render error]" {
		t.Fatalf("got %q", out)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(out, "SECRET123") || strings.Contains(string(data), "SECRET123") {
		t.Fatalf("secret leaked: %q %s", out, data)
	}
}

func TestRenderGuardMarkdownBlock(t *testing.T) {
	m, _ := guardModel(t)
	m.add(blockAssistant, "# Title\n\nbody **bold**")
	renderBlockHook = func(b *block) {
		if b.kind == blockAssistant {
			panic("markdown boom")
		}
	}
	m.add(blockInfo, "after-text")
	if out := ansi.Strip(m.renderBlock(m.blocks[0], 80)); out != "[render error]" {
		t.Fatalf("got %q", out)
	}
	if out := ansi.Strip(m.renderBlock(m.blocks[1], 80)); !strings.Contains(out, "after-text") {
		t.Fatalf("got %q", out)
	}
}
