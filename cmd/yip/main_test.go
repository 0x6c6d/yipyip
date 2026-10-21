package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/session"
	"yipyip/internal/tui"
)

func TestVersionFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(--version) = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if !regexp.MustCompile(`^yip `).MatchString(stdout.String()) {
		t.Fatalf("stdout = %q, want prefix %q", stdout.String(), "yip ")
	}
}

func TestHelpFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(--help) = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	for _, c := range []string{"/model", "/effort", "/mode", "/agents", "/init", "/compact",
		"/undo", "/new", "/clear", "/exit", "/quit", "/help"} {
		if !strings.Contains(stdout.String(), c) {
			t.Errorf("help output does not contain %q", c)
		}
	}
}

func TestHelpFlagNewKeys(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(--help) = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	for _, want := range []string{"Up/Down", "Ctrl+Home", "Ctrl+End", "Tab", "queue"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help output does not contain %q", want)
		}
	}
}

func TestUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--bogus"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run(--bogus) = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "usage") {
		t.Fatalf("stderr = %q, want it to mention usage", stderr.String())
	}
}

func TestConfigErrorExitCode(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "yipyip.json"), []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run(nil) = %d, want 1 (stderr: %q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "yipyip.json") {
		t.Fatalf("stderr = %q, want it to name yipyip.json", stderr.String())
	}
}

func TestNewModelStartsAuto(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, dir := filepath.Join(base, "home"), filepath.Join(base, "start")
	for _, d := range []string{filepath.Dir(config.GlobalPath(home)), dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(config.GlobalPath(home), []byte(`{"provider": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(home, dir, func(string) string { return "" })
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	sess, err := agent.NewSession(agent.Options{Config: cfg, StartDir: dir})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(sess.Cancel)
	m := newModel(dir, sess)
	if sess.Mode() != agent.ModeAuto {
		t.Fatalf("mode = %q, want %q", sess.Mode(), agent.ModeAuto)
	}
	if line := ansi.Strip(m.StatusLine()); !strings.Contains(line, "auto (shift+tab to toggle)") {
		t.Errorf("status line = %q, want it to show auto mode", line)
	}
}

// resumeStartDir builds a temp HOME with a minimal global config and a temp
// start directory, makes them HOME and the working directory, and returns the
// (symlink-resolved) start directory.
func resumeStartDir(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, dir := filepath.Join(base, "home"), filepath.Join(base, "start")
	for _, d := range []string{filepath.Dir(config.GlobalPath(home)), dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(config.GlobalPath(home), []byte(`{"provider": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Chdir(dir)
	return dir
}

// saveResumeSession stores one valid session in dir and returns it (with
// its derived id).
func saveResumeSession(t *testing.T, dir string) *session.SessionData {
	t.Helper()
	store, err := session.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	data := session.NewSessionData()
	data.StartDir = dir
	data.Mode = "auto"
	data.Conversation = []session.Conversation{
		{Role: "user", Content: "hello there"},
		{Role: "assistant", Content: "hi, how can I help?"},
	}
	data.History = []string{"hello there"}
	data.Title = session.Title("hello there")
	if err := store.Save(data); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return data
}

// runResume drives the flag parsing and the hand-over without running a
// program: the launch is faked and captures the UI options.
func runResume(t *testing.T, args []string, tty bool) (opts tui.Options, launched bool, code int, stderr string) {
	t.Helper()
	var stdout, errb bytes.Buffer
	code = runWith(args, &stdout, &errb, deps{stdinTTY: tty, launch: func(o tui.Options, _ io.Writer) int {
		opts, launched = o, true
		if o.Session != nil {
			o.Session.Cancel()
		}
		return 0
	}})
	return opts, launched, code, errb.String()
}

func TestResumeFlagParsesId(t *testing.T) {
	dir := resumeStartDir(t)
	saved := saveResumeSession(t, dir)
	for _, args := range [][]string{
		{"--resume", saved.ID},
		{"-c", saved.ID},
		{"--resume=" + saved.ID},
	} {
		opts, launched, code, stderr := runResume(t, args, false)
		if code != 0 || !launched {
			t.Fatalf("%v: code = %d, launched = %v (stderr %q)", args, code, launched, stderr)
		}
		if opts.ResumeData == nil || opts.ResumeData.ID != saved.ID {
			t.Fatalf("%v: ResumeData = %+v, want id %s", args, opts.ResumeData, saved.ID)
		}
		if opts.Session == nil {
			t.Fatalf("%v: Session is nil, want the fresh agent session", args)
		}
	}
	opts, launched, code, stderr := runResume(t, nil, true)
	if code != 0 || !launched {
		t.Fatalf("plain yip: code = %d, launched = %v (stderr %q)", code, launched, stderr)
	}
	if opts.ResumeData != nil || opts.ResumePick {
		t.Fatalf("plain yip: ResumeData = %+v, ResumePick = %v, want nil/false", opts.ResumeData, opts.ResumePick)
	}
}

func TestResumeUnknownIdExit1(t *testing.T) {
	dir := resumeStartDir(t)
	saveResumeSession(t, dir) // the store exists, the id is just not in it
	for _, args := range [][]string{{"--resume", "deadbeefdead"}, {"-c", "deadbeefdead"}} {
		_, launched, code, stderr := runResume(t, args, true)
		if code != 1 || launched {
			t.Fatalf("%v: code = %d, launched = %v, want 1 and no launch", args, code, launched)
		}
		if strings.TrimSpace(stderr) != "unknown session deadbeefdead" {
			t.Fatalf("%v: stderr = %q, want exactly the unknown-session line", args, stderr)
		}
		if strings.Contains(stderr, ".yipyip/sessions") || strings.Contains(stderr, dir) {
			t.Fatalf("%v: stderr %q leaks the sessions path", args, stderr)
		}
	}
	// An invalid file under a valid id is also just an unknown session.
	bad := filepath.Join(dir, ".yipyip", "sessions", "0123456789ab.json")
	if err := os.WriteFile(bad, []byte(`{"version": 2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, code, stderr := runResume(t, []string{"--resume=0123456789ab"}, false)
	if code != 1 || strings.TrimSpace(stderr) != "unknown session 0123456789ab" {
		t.Fatalf("version mismatch: code = %d, stderr = %q", code, stderr)
	}
}

func TestResumeWithIdLoadsData(t *testing.T) {
	dir := resumeStartDir(t)
	saved := saveResumeSession(t, dir)
	opts, launched, code, stderr := runResume(t, []string{"-c", saved.ID}, true)
	if code != 0 || !launched {
		t.Fatalf("code = %d, launched = %v (stderr %q)", code, launched, stderr)
	}
	got := opts.ResumeData
	if got == nil {
		t.Fatal("ResumeData is nil")
	}
	if len(got.Conversation) != len(saved.Conversation) {
		t.Fatalf("conversation = %+v, want %+v", got.Conversation, saved.Conversation)
	}
	for i := range saved.Conversation {
		if got.Conversation[i].Role != saved.Conversation[i].Role || got.Conversation[i].Content != saved.Conversation[i].Content {
			t.Fatalf("conversation[%d] = %+v, want %+v", i, got.Conversation[i], saved.Conversation[i])
		}
	}
	if strings.Join(got.History, "\n") != strings.Join(saved.History, "\n") {
		t.Fatalf("history = %q, want %q", got.History, saved.History)
	}
	if got.StartDir != dir || got.Title != saved.Title {
		t.Fatalf("startdir/title = %q/%q, want %q/%q", got.StartDir, got.Title, dir, saved.Title)
	}
	if opts.ResumePick {
		t.Fatal("ResumePick set although an id was given")
	}
}

func TestResumeNoIdNonTTYStartsFresh(t *testing.T) {
	dir := resumeStartDir(t)
	saveResumeSession(t, dir)
	for _, args := range [][]string{{"--resume"}, {"-c"}} {
		opts, launched, code, stderr := runResume(t, args, false)
		if code != 0 || !launched {
			t.Fatalf("%v: code = %d, launched = %v (stderr %q)", args, code, launched, stderr)
		}
		if opts.ResumeData != nil || opts.ResumePick {
			t.Fatalf("%v: ResumeData = %+v, ResumePick = %v, want a fresh start", args, opts.ResumeData, opts.ResumePick)
		}
		if opts.StdinTTY {
			t.Fatalf("%v: StdinTTY = true, want false", args)
		}
	}
	// Control: with a tty the same store asks for the picker.
	opts, _, _, _ := runResume(t, []string{"--resume"}, true)
	if !opts.ResumePick || opts.ResumeData != nil {
		t.Fatalf("tty: ResumePick = %v, ResumeData = %+v, want picker and no data", opts.ResumePick, opts.ResumeData)
	}
}

func TestResumeNoIdEmptyDirStartsFresh(t *testing.T) {
	dir := resumeStartDir(t)
	opts, launched, code, stderr := runResume(t, []string{"--resume"}, true)
	if code != 0 || !launched {
		t.Fatalf("code = %d, launched = %v (stderr %q)", code, launched, stderr)
	}
	if opts.ResumePick || opts.ResumeData != nil {
		t.Fatalf("no sessions dir: ResumePick = %v, ResumeData = %+v, want a fresh start", opts.ResumePick, opts.ResumeData)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".yipyip", "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts, _, _, _ = runResume(t, []string{"-c"}, true)
	if opts.ResumePick || opts.ResumeData != nil {
		t.Fatalf("empty sessions dir: ResumePick = %v, ResumeData = %+v, want a fresh start", opts.ResumePick, opts.ResumeData)
	}
}

func TestResumeHelpMentionsFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run(--help) = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	for _, want := range []string{"--resume [id]", "-c"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help output does not contain %q", want)
		}
	}
}

func TestHelpFlagMentionsShiftEnter(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run(--help) = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Shift+Enter") {
		t.Errorf("help output does not contain %q", "Shift+Enter")
	}
}
