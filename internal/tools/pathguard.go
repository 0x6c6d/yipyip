// Package tools implements the file and shell tools available to the agent.
//
// File paths go through PathGuard: writes (and every caller that must stay
// inside) use Resolve, which confines the result to the start directory;
// reads use ResolveRead, which resolves the path the same way but may end
// anywhere on the file system.
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

// PathGuard resolves paths relative to the start directory and confines
// writes to it: Resolve (write mode) rejects every path that resolves outside
// the start directory, ResolveRead (read mode) resolves the same way without
// that check. It is the only place that resolves agent-supplied paths and
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

// Resolve is the write-mode resolution, used by every caller that writes or
// must stay inside the start directory. It returns the absolute,
// symlink-resolved form of p (relative paths are taken relative to the root).
// It fails if p is empty, contains a NUL byte or a dangling symlink, or
// resolves outside the root; escapes wrap ErrOutsideStartDir. Non-existent
// trailing components are allowed so that new files can be created.
func (g *PathGuard) Resolve(p string) (string, error) {
	resolved, err := g.resolve(p)
	if err != nil {
		return "", err
	}
	if !g.inside(resolved) {
		return "", fmt.Errorf("%w: %s", ErrOutsideStartDir, p)
	}
	return resolved, nil
}

// ResolveRead is the read-mode resolution. It resolves p exactly like
// Resolve (empty or NUL paths and dangling symlinks are errors, relative
// paths start at the root, non-existent trailing components are allowed)
// but without the inside-the-root check: absolute paths, ".." and symlinks
// may resolve anywhere on the file system.
func (g *PathGuard) ResolveRead(p string) (string, error) {
	return g.resolve(p)
}

// resolve returns the absolute, clean, symlink-resolved form of p: relative
// paths are joined to the root, the deepest existing ancestor is resolved with
// filepath.EvalSymlinks (a dangling symlink or broken chain is an error and is
// never followed) and the non-existent rest is appended. It does not check
// where the result lies.
func (g *PathGuard) resolve(p string) (string, error) {
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
	return filepath.Clean(resolved), nil
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
