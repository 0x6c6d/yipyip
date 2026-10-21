package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"yipyip/internal/tools"
)

// idPattern is the public session id: 12 lowercase hex chars — the first
// 3 bytes of the SHA-256 of the first save's content, hex-encoded (A3).
var idPattern = regexp.MustCompile(`^[0-9a-f]{12}$`)

// sessionsRel is the sessions directory relative to the start directory.
const sessionsRel = ".yipyip/sessions"

// Store persists the sessions of one start directory in
// <startdir>/.yipyip/sessions (created lazily, mode 0755). Every path goes
// through internal/tools' PathGuard: a <startdir>/.yipyip (or any ancestor)
// symlink pointing outside the resolved start directory makes every method
// refuse. The store never runs a shell command and never reads config or
// env values.
type Store struct {
	guard *tools.PathGuard
}

// NewStore returns a session store for startDir. startDir is made absolute
// and symlink-resolved exactly as internal/tools' PathGuard does; it must
// exist and be a directory.
func NewStore(startDir string) (*Store, error) {
	g, err := tools.NewPathGuard(startDir)
	if err != nil {
		return nil, fmt.Errorf("session store: %w", err)
	}
	return &Store{guard: g}, nil
}

// resolve returns the absolute, confined path of name, a path relative to
// the sessions directory ("" = the directory itself, which need not exist).
// It fails when a component of .yipyip/sessions/name (or an ancestor) is a
// symlink resolving outside the start directory; escapes wrap
// tools.ErrOutsideStartDir.
func (s *Store) resolve(name string) (string, error) {
	p, err := s.guard.Resolve(filepath.Join(sessionsRel, name))
	if err != nil {
		return "", fmt.Errorf("session store: %w", err)
	}
	return p, nil
}

// filePath returns the path a session id would be stored at, for error
// messages only (it is not resolved through the guard).
func (s *Store) filePath(id string) string {
	return filepath.Join(s.guard.Root(), sessionsRel, id+".json")
}

// checkID rejects session ids that are not 12 lowercase hex chars, naming
// the file the id would map to.
func (s *Store) checkID(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("session file %s: invalid session id %q (want 12 hex chars)", s.filePath(id), id)
	}
	return nil
}

// Save persists data. The write is atomic: a temp file in the sessions
// directory (os.CreateTemp, mode 0600), full JSON write, Sync, Close, then
// os.Rename over the target. When data.ID is empty, the id is derived first
// (A3): data is written to a temp name, the SHA-256 of that content is
// taken and data.ID becomes the first 3 bytes in hex (12 chars), then the
// final <id>.json is written; a set data.ID is kept and written directly.
// Version and Updated are always stamped; Created is stamped when empty.
func (s *Store) Save(data *SessionData) error {
	dir, err := s.resolve("")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("session store: create %s: %w", dir, err)
	}
	if data.ID != "" {
		if err := s.checkID(data.ID); err != nil {
			return err
		}
	}
	stamp := now().UTC().Format(time.RFC3339)
	data.Version = FileVersion
	if data.Created == "" {
		data.Created = stamp
	}
	data.Updated = stamp
	if data.ID == "" {
		tmp, sum, err := writeTemp(dir, data)
		if err != nil {
			return err
		}
		if err := os.Remove(tmp); err != nil {
			return fmt.Errorf("session store: remove %s: %w", tmp, err)
		}
		data.ID = hex.EncodeToString(sum[:6])
	}
	return writeAtomic(dir, data.ID+".json", data)
}

// Load reads and parses <id>.json. A missing file, a version other than 1
// (absent, 0 or 2+) or invalid JSON yields an error naming the file path;
// it never panics. Unknown fields are ignored.
func (s *Store) Load(id string) (*SessionData, error) {
	if err := s.checkID(id); err != nil {
		return nil, err
	}
	path, err := s.resolve(id + ".json")
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("session file %s: %w", path, err)
	}
	var data SessionData
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, fmt.Errorf("session file %s: invalid JSON: %v", path, err)
	}
	if data.Version != FileVersion {
		return nil, fmt.Errorf("session file %s: unsupported version %d (want %d)", path, data.Version, FileVersion)
	}
	return &data, nil
}

// List returns every valid session file, newest first by Updated (falling
// back to Created). Unreadable or invalid files are skipped; a missing
// sessions directory yields an empty list, not an error.
func (s *Store) List() ([]SessionData, error) {
	dir, err := s.resolve("")
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("session store: read %s: %w", dir, err)
	}
	var list []SessionData
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := s.Load(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue // unreadable or invalid: skipped, not fatal
		}
		list = append(list, *data)
	}
	sort.SliceStable(list, func(i, j int) bool { return newerFirst(list[i], list[j]) })
	return list, nil
}

// Delete removes <id>.json. A missing file yields a normal error naming the
// file.
func (s *Store) Delete(id string) error {
	if err := s.checkID(id); err != nil {
		return err
	}
	path, err := s.resolve(id + ".json")
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("session file %s: %w", path, err)
	}
	return nil
}

// writeTemp writes data as indented JSON to a fresh temp file in dir
// (os.CreateTemp, mode 0600, Sync, Close) and returns the temp file's path
// and the SHA-256 of its content. The caller owns the temp file.
func writeTemp(dir string, data *SessionData) (string, [sha256.Size]byte, error) {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", [sha256.Size]byte{}, fmt.Errorf("session store: marshal: %w", err)
	}
	b = append(b, '\n')
	sum := sha256.Sum256(b)
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return "", sum, fmt.Errorf("session store: create temp file in %s: %w", dir, err)
	}
	name := f.Name()
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		os.Remove(name)
		return "", sum, fmt.Errorf("session store: chmod %s: %w", name, err)
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(name)
		return "", sum, fmt.Errorf("session store: write %s: %w", name, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(name)
		return "", sum, fmt.Errorf("session store: sync %s: %w", name, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(name)
		return "", sum, fmt.Errorf("session store: close %s: %w", name, err)
	}
	return name, sum, nil
}

// writeAtomic writes data to dir/name: a temp file in the same directory,
// then os.Rename over the target.
func writeAtomic(dir, name string, data *SessionData) error {
	tmp, _, err := writeTemp(dir, data)
	if err != nil {
		return err
	}
	target := filepath.Join(dir, name)
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("session store: rename to %s: %w", target, err)
	}
	return nil
}

// sortKey returns the timestamp a session is ordered by.
func sortKey(d SessionData) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339, d.Updated); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, d.Created); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// newerFirst reports whether a sorts before b (newest first).
func newerFirst(a, b SessionData) bool {
	ta, oka := sortKey(a)
	tb, okb := sortKey(b)
	switch {
	case oka && okb:
		return ta.After(tb)
	case oka != okb:
		return oka
	default:
		return strings.Compare(a.Updated+a.Created, b.Updated+b.Created) > 0
	}
}
