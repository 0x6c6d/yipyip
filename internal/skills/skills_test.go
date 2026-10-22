package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// startDir returns a symlink-resolved temp base and a start directory below it.
func startDir(t *testing.T) (string, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	start := filepath.Join(base, "start")
	if err := os.MkdirAll(start, 0o755); err != nil {
		t.Fatal(err)
	}
	return base, start
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// warned reports whether some single-line warning names folder.
func warned(warns []string, folder string) bool {
	for _, w := range warns {
		if strings.HasPrefix(w, "skill "+folder+":") && !strings.Contains(w, "\n") {
			return true
		}
	}
	return false
}

func TestSkillLoadProject(t *testing.T) {
	_, start := startDir(t)
	dir := ProjectDir(start)
	writeFile(t, filepath.Join(dir, "zeta", "SKILL.md"), "---\nname: alpha\ndescription: First one\n---\n\nDo alpha.\n")
	writeFile(t, filepath.Join(dir, "beta", "SKILL.md"), "---\ndescription: Second\n---\nDo beta.")
	writeFile(t, filepath.Join(dir, "notes.txt"), "not a skill")
	if err := os.MkdirAll(filepath.Join(dir, "nofile"), 0o755); err != nil {
		t.Fatal(err)
	}

	list, warns := Load("", start)
	if len(warns) != 0 {
		t.Fatalf("warnings: %q", warns)
	}
	want := []Skill{
		{Name: "alpha", Description: "First one", Body: "Do alpha.", Source: Project},
		{Name: "beta", Description: "Second", Body: "Do beta.", Source: Project},
	}
	if len(list) != len(want) {
		t.Fatalf("got %+v, want %+v", list, want)
	}
	for i := range want {
		if list[i] != want[i] {
			t.Errorf("skill %d: got %+v, want %+v", i, list[i], want[i])
		}
	}
	if got := GlobalDir("/h"); got != filepath.Join("/h", ".config", "yipyip", "skills") {
		t.Errorf("GlobalDir = %q", got)
	}
}

func TestSkillFrontmatter(t *testing.T) {
	in := "---\r\n# comment\r\n\r\nname: \"quoted-name\"\r\ndescription: 'a: b'\r\nversion: 3\r\n---\r\nLine one\r\nLine two\r\n"
	s, err := Parse("folder", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "quoted-name" || s.Description != "a: b" || s.Body != "Line one\nLine two" {
		t.Fatalf("Parse: %+v", s)
	}
	s, err = Parse("my-skill", []byte("---\n---\n"))
	if err != nil || s.Name != "my-skill" || s.Description != "" || s.Body != "" {
		t.Fatalf("defaults: %+v %v", s, err)
	}
	bad := map[string]string{
		"missing":      "name: x\n---\nbody\n",
		"unclosed":     "---\nname: x\nbody\n",
		"no colon":     "---\njust words\n---\n",
		"invalid name": "---\nname: Bad_Name\n---\n",
		"bad folder":   "---\n---\n",
	}
	for label, in := range bad {
		folder := "ok"
		if label == "bad folder" {
			folder = "Has Space"
		}
		if _, err := Parse(folder, []byte(in)); err == nil {
			t.Errorf("%s: Parse accepted %q", label, in)
		}
	}
}

func TestSkillMissingDir(t *testing.T) {
	_, start := startDir(t)
	if list, warns := Load("", start); len(list) != 0 || len(warns) != 0 {
		t.Fatalf("missing dir: %+v %q", list, warns)
	}
	if list, warns := Load("", ""); len(list) != 0 || len(warns) != 0 {
		t.Fatalf("empty start dir: %+v %q", list, warns)
	}
}

func TestSkillSymlinkEscape(t *testing.T) {
	base, start := startDir(t)
	dir := ProjectDir(start)
	out := filepath.Join(base, "outside")
	writeFile(t, filepath.Join(out, "esc", "SKILL.md"), "---\nname: escdir\n---\nleak\n")
	writeFile(t, filepath.Join(out, "file.md"), "---\nname: escfile\n---\nleak\n")
	writeFile(t, filepath.Join(dir, "good", "SKILL.md"), "---\n---\nfine\n")
	if err := os.MkdirAll(filepath.Join(dir, "linkfile"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(out, "esc"), filepath.Join(dir, "linkdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(out, "file.md"), filepath.Join(dir, "linkfile", "SKILL.md")); err != nil {
		t.Fatal(err)
	}

	list, warns := Load("", start)
	if len(list) != 1 || list[0].Name != "good" {
		t.Fatalf("want only good, got %+v", list)
	}
	for _, f := range []string{"linkdir", "linkfile"} {
		if !warned(warns, f) {
			t.Errorf("no warning for %s in %q", f, warns)
		}
	}
	if len(warns) != 2 {
		t.Errorf("want 2 warnings, got %q", warns)
	}
}

func TestSkillTooLarge(t *testing.T) {
	_, start := startDir(t)
	dir := ProjectDir(start)
	writeFile(t, filepath.Join(dir, "big", "SKILL.md"), "---\n---\n"+strings.Repeat("x", maxSkillSize))
	writeFile(t, filepath.Join(dir, "edge", "SKILL.md"), "---\n---\n"+strings.Repeat("y", maxSkillSize-8))

	list, warns := Load("", start)
	if len(list) != 1 || list[0].Name != "edge" {
		t.Fatalf("want only edge, got %+v", list)
	}
	if len(warns) != 1 || !warned(warns, "big") {
		t.Fatalf("warnings: %q", warns)
	}
}

// homeStart returns a temp home and start directory (both exist, no skills yet).
func homeStart(t *testing.T) (string, string) {
	t.Helper()
	base, start := startDir(t)
	home := filepath.Join(base, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	return home, start
}

// find returns the skill called name, or nil.
func find(list []Skill, name string) *Skill {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}

func TestSkillGlobalOnly(t *testing.T) {
	home, start := homeStart(t)
	g := GlobalDir(home)
	writeFile(t, filepath.Join(g, "lint", "SKILL.md"), "---\ndescription: Lint it\n---\nRun lint.\n")
	writeFile(t, filepath.Join(g, "a-first", "SKILL.md"), "---\n---\nA\n")

	list, warns := Load(home, start)
	if len(warns) != 0 {
		t.Fatalf("warnings: %q", warns)
	}
	want := []Skill{
		{Name: "a-first", Body: "A", Source: Global},
		{Name: "lint", Description: "Lint it", Body: "Run lint.", Source: Global},
	}
	if len(list) != len(want) || list[0] != want[0] || list[1] != want[1] {
		t.Fatalf("got %+v, want %+v", list, want)
	}
}

func TestSkillProjectOnly(t *testing.T) {
	home, start := homeStart(t)
	writeFile(t, filepath.Join(ProjectDir(start), "deploy", "SKILL.md"), "---\ndescription: Ship\n---\nDeploy.\n")

	list, warns := Load(home, start)
	if len(warns) != 0 {
		t.Fatalf("warnings: %q", warns)
	}
	if len(list) != 1 || list[0] != (Skill{Name: "deploy", Description: "Ship", Body: "Deploy.", Source: Project}) {
		t.Fatalf("got %+v", list)
	}
}

func TestSkillProjectOverrides(t *testing.T) {
	home, start := homeStart(t)
	g, p := GlobalDir(home), ProjectDir(start)
	writeFile(t, filepath.Join(g, "review", "SKILL.md"), "---\ndescription: global review\n---\nGLOBAL BODY\n")
	writeFile(t, filepath.Join(g, "only-global", "SKILL.md"), "---\n---\nG\n")
	// The project folder name differs; the frontmatter name decides the override.
	writeFile(t, filepath.Join(p, "rev", "SKILL.md"), "---\nname: review\ndescription: project review\n---\nPROJECT BODY\n")
	writeFile(t, filepath.Join(p, "only-project", "SKILL.md"), "---\n---\nP\n")

	list, warns := Load(home, start)
	if len(warns) != 0 {
		t.Fatalf("warnings: %q", warns)
	}
	names := []string{}
	for _, s := range list {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "only-global,only-project,review" {
		t.Fatalf("names: %v", names)
	}
	r := find(list, "review")
	if r.Body != "PROJECT BODY" || r.Description != "project review" || r.Source != Project {
		t.Errorf("review: %+v", r)
	}
	if s := find(list, "only-global"); s.Source != Global {
		t.Errorf("only-global: %+v", s)
	}
	if s := find(list, "only-project"); s.Source != Project {
		t.Errorf("only-project: %+v", s)
	}
}

func TestSkillBadSkipped(t *testing.T) {
	home, start := homeStart(t)
	g, p := GlobalDir(home), ProjectDir(start)
	writeFile(t, filepath.Join(g, "Bad_Global", "SKILL.md"), "---\n---\nx\n")
	writeFile(t, filepath.Join(g, "gnofm", "SKILL.md"), "just text\n")
	writeFile(t, filepath.Join(g, "gunclosed", "SKILL.md"), "---\nname: gunclosed\n")
	writeFile(t, filepath.Join(g, "gdup-a", "SKILL.md"), "---\nname: gdup\n---\nfirst\n")
	writeFile(t, filepath.Join(g, "gdup-b", "SKILL.md"), "---\nname: gdup\n---\nsecond\n")
	writeFile(t, filepath.Join(g, "gfine", "SKILL.md"), "---\n---\nok\n")
	writeFile(t, filepath.Join(p, "badname", "SKILL.md"), "---\nname: Has Space\n---\nx\n")
	writeFile(t, filepath.Join(p, "pnofm", "SKILL.md"), "no frontmatter\n")
	writeFile(t, filepath.Join(p, "punclosed", "SKILL.md"), "---\nname: punclosed\n")
	writeFile(t, filepath.Join(p, "pfine", "SKILL.md"), "---\n---\nok\n")

	list, warns := Load(home, start)
	names := []string{}
	for _, s := range list {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "gdup,gfine,pfine" || find(list, "gdup").Body != "first" {
		t.Fatalf("list: %+v", list)
	}
	global := []string{"Bad_Global", "gnofm", "gunclosed", "gdup-b"}
	project := []string{"badname", "pnofm", "punclosed"}
	if len(warns) != len(global)+len(project) {
		t.Fatalf("want %d warnings, got %q", len(global)+len(project), warns)
	}
	for _, f := range global {
		if !warned(warns, "global/"+f) {
			t.Errorf("no global warning for %s in %q", f, warns)
		}
	}
	for _, f := range project {
		if !warned(warns, f) {
			t.Errorf("no warning for %s in %q", f, warns)
		}
	}
}

func TestSkillMissingDirs(t *testing.T) {
	home, start := homeStart(t)
	if list, warns := Load(home, start); len(list) != 0 || len(warns) != 0 {
		t.Fatalf("missing dirs: %+v %q", list, warns)
	}
	if list, warns := Load("", start); len(list) != 0 || len(warns) != 0 {
		t.Fatalf("empty home: %+v %q", list, warns)
	}
	if list, warns := Load(filepath.Join(home, "nope"), filepath.Join(start, "nope")); len(list) != 0 || len(warns) != 0 {
		t.Fatalf("nonexistent home and start: %+v %q", list, warns)
	}
}
