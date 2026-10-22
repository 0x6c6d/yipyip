// Package skills loads skills: folders holding a SKILL.md with a small
// frontmatter header and a Markdown body. The package only reads files.
package skills

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"yipyip/internal/tools"
)

// Source says where a skill was loaded from.
type Source string

const (
	Global  Source = "global"
	Project Source = "project"
)

// Skill is one parsed SKILL.md.
type Skill struct {
	Name, Description, Body string
	Source                  Source
}

// maxSkillSize is the largest SKILL.md that is loaded.
const maxSkillSize = 256 * 1024

var nameRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// GlobalDir returns the global skills directory below home.
func GlobalDir(home string) string { return filepath.Join(home, ".config", "yipyip", "skills") }

// ProjectDir returns the project skills directory below startDir.
func ProjectDir(startDir string) string { return filepath.Join(startDir, ".yipyip", "skills") }

// Parse parses one SKILL.md; folder is the name of the skill's folder.
func Parse(folder string, data []byte) (Skill, error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return Skill{}, errors.New("missing frontmatter (first line must be ---)")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return Skill{}, errors.New("unclosed frontmatter (no closing ---)")
	}
	s := Skill{Name: folder}
	for i := 1; i < end; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return Skill{}, fmt.Errorf("frontmatter line %d: missing ':'", i+1)
		}
		key, value = strings.TrimSpace(key), unquote(strings.TrimSpace(value))
		switch key {
		case "name":
			s.Name = value
		case "description":
			s.Description = value
		}
	}
	if !nameRe.MatchString(s.Name) {
		return Skill{}, fmt.Errorf("invalid name %q (want [a-z0-9-]+)", s.Name)
	}
	s.Body = strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	return s, nil
}

// unquote removes one pair of matching double or single quotes around v.
func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// Load returns the loaded skills sorted by Name and one warning line per skipped entry.
// The global directory below home is read first (skipped when home is ""); a project
// skill with the same Name replaces the global one. Missing directories are fine.
func Load(home, startDir string) ([]Skill, []string) {
	var warns []string
	byName := map[string]Skill{}
	if home != "" {
		for _, s := range loadGlobal(home, &warns) {
			byName[s.Name] = s
		}
	}
	for _, s := range loadProject(startDir, &warns) {
		byName[s.Name] = s
	}
	list := make([]Skill, 0, len(byName))
	for _, s := range byName {
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, warns
}

// loadGlobal reads <home>/.config/yipyip/skills. The directory is the user's own
// and is read directly, with the same size limit and parse rules as the project.
func loadGlobal(home string, warns *[]string) []Skill {
	dir := GlobalDir(home)
	fi, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		*warns = append(*warns, warning("global", err))
		return nil
	}
	if !fi.IsDir() {
		return nil
	}
	return loadDir(dir, "global", "global/", Global, func(p string) (string, error) { return p, nil }, warns)
}

// loadProject reads <startDir>/.yipyip/skills; every path goes through the PathGuard.
func loadProject(startDir string, warns *[]string) []Skill {
	if startDir == "" {
		return nil
	}
	guard, err := tools.NewPathGuard(startDir)
	if err != nil {
		return nil
	}
	dir, err := guard.Resolve(ProjectDir(guard.Root()))
	if err != nil {
		*warns = append(*warns, warning(filepath.Join(".yipyip", "skills"), err))
		return nil
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return nil
	}
	return loadDir(dir, filepath.Join(".yipyip", "skills"), "", Project, guard.Resolve, warns)
}

// loadDir loads every skill folder of dir in folder-name order. resolve checks
// (and may rewrite) each path before it is opened; label names dir and prefix
// goes before the folder in warnings. Within dir a duplicate Name keeps the first folder.
func loadDir(dir, label, prefix string, src Source, resolve func(string) (string, error), warns *[]string) []Skill {
	entries, err := os.ReadDir(dir) // sorted by folder name
	if err != nil {
		*warns = append(*warns, warning(label, err))
		return nil
	}
	var list []Skill
	seen := map[string]string{}
	for _, e := range entries {
		folder := e.Name()
		s, ok, err := loadOne(resolve, dir, folder)
		if err != nil {
			*warns = append(*warns, warning(prefix+folder, err))
			continue
		}
		if !ok {
			continue
		}
		if first, dup := seen[s.Name]; dup {
			*warns = append(*warns, warning(prefix+folder, fmt.Errorf("duplicate name %q (already defined by %s)", s.Name, first)))
			continue
		}
		seen[s.Name] = folder
		s.Source = src
		list = append(list, s)
	}
	return list
}

// loadOne loads dir/folder/SKILL.md. ok is false (with a nil error) for entries
// that are silently ignored: non-directories and folders without SKILL.md.
func loadOne(resolve func(string) (string, error), dir, folder string) (Skill, bool, error) {
	fdir, err := resolve(filepath.Join(dir, folder))
	if err != nil {
		return Skill{}, false, err
	}
	fi, err := os.Stat(fdir)
	if err != nil || !fi.IsDir() {
		return Skill{}, false, nil
	}
	file := filepath.Join(fdir, "SKILL.md")
	if _, err := os.Lstat(file); errors.Is(err, fs.ErrNotExist) {
		return Skill{}, false, nil
	}
	file, err = resolve(file)
	if err != nil {
		return Skill{}, false, err
	}
	data, err := readBounded(file)
	if err != nil {
		return Skill{}, false, err
	}
	s, err := Parse(folder, data)
	if err != nil {
		return Skill{}, false, err
	}
	return s, true, nil
}

// readBounded reads a regular file of at most maxSkillSize bytes, never
// reading more than maxSkillSize+1 bytes.
func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("SKILL.md is not a regular file")
	}
	if fi.Size() > maxSkillSize {
		return nil, fmt.Errorf("SKILL.md is larger than %d KiB", maxSkillSize/1024)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxSkillSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSkillSize {
		return nil, fmt.Errorf("SKILL.md is larger than %d KiB", maxSkillSize/1024)
	}
	return data, nil
}

// warning formats one single-line warning naming the folder.
func warning(folder string, err error) string {
	return strings.Join(strings.Fields(fmt.Sprintf("skill %s: %s", folder, err)), " ")
}
