// Package tools implements the file and shell tools available to the agent.
package tools

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ErrOutsideStartDir is wrapped by every error that reports a path escaping
// the start directory.
var ErrOutsideStartDir = errors.New("path is outside the start directory")

// PathGuard confines paths to the start directory. It is the only place that
// decides whether a path lies inside the start directory.
type PathGuard struct {
	root string
}

// NewPathGuard returns a guard for startDir. startDir is made absolute and
// symlink-resolved; it must exist and be a directory.
func NewPathGuard(startDir string) (*PathGuard, error) {
	if startDir == "" {
		return nil, errors.New("pathguard: empty start directory")
	}
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return nil, fmt.Errorf("pathguard: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("pathguard: %w", err)
	}
	fi, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("pathguard: %w", err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("pathguard: start directory %s is not a directory", startDir)
	}
	return &PathGuard{root: filepath.Clean(resolved)}, nil
}

// Root returns the absolute, symlink-resolved start directory.
func (g *PathGuard) Root() string { return g.root }

// Resolve returns the absolute, symlink-resolved form of p (relative paths
// are taken relative to the root). It fails if p is empty, contains a
// dangling symlink, or resolves outside the root; escapes wrap
// ErrOutsideStartDir. Non-existent trailing components are allowed so that
// new files can be created.
func (g *PathGuard) Resolve(p string) (string, error) {
	if p == "" {
		return "", errors.New("empty path")
	}
	if strings.IndexByte(p, 0) >= 0 {
		return "", fmt.Errorf("invalid path (NUL byte): %q", p)
	}
	full := p
	if !filepath.IsAbs(full) {
		full = filepath.Join(g.root, full)
	}
	full = filepath.Clean(full)

	// Walk up to the deepest existing ancestor.
	existing := full
	var rest []string
	for {
		_, err := os.Lstat(existing)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
			return "", fmt.Errorf("resolve %s: %w", p, err)
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("resolve %s: no existing ancestor", p)
		}
		rest = append(rest, filepath.Base(existing))
		existing = parent
	}

	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		// Dangling symlink or broken chain: never follow it.
		return "", fmt.Errorf("resolve %s: %w", p, err)
	}
	for i := len(rest) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, rest[i])
	}
	resolved = filepath.Clean(resolved)

	if !g.inside(resolved) {
		return "", fmt.Errorf("%w: %s", ErrOutsideStartDir, p)
	}
	return resolved, nil
}

// inside reports whether the absolute, clean path abs equals the root or lies
// below it.
func (g *PathGuard) inside(abs string) bool {
	rel, err := filepath.Rel(g.root, abs)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	return true
}
