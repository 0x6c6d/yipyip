package agent

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"yipyip/internal/provider"
)

// ErrNothingToUndo is returned by Undo when no turn is left to undo.
var ErrNothingToUndo = errors.New("nothing to undo")

// UndoResult describes what one Undo did.
type UndoResult struct {
	Restored        []string // paths relative to the start dir ("/"-separated), sorted
	Deleted         []string // files created in the turn and now removed, sorted
	BashNotReverted []string // bash commands of the undone turn, in execution order
}

// fileSnapshot is the state of a file before the first write of a turn.
type fileSnapshot struct {
	existed    bool
	data       []byte
	mode       fs.FileMode
	unreadable bool // the old content could not be read: never touch the file on undo
}

// turnRecord is the undo journal entry of one turn (memory only).
type turnRecord struct {
	gen     uint64 // session generation the turn belongs to
	histLen int    // history length before the turn's user message

	// Guarded by Session.jmu.
	snaps map[string]fileSnapshot // absolute path -> state before the turn
	bash  []string                // bash commands that ran, in order
}

// openTurnRecord appends a journal record for a turn starting now. The
// caller holds s.mu and has not yet appended the turn's user message.
func (s *Session) openTurnRecord() {
	s.journal = append(s.journal, &turnRecord{
		gen:     s.gen,
		histLen: len(s.history),
		snaps:   map[string]fileSnapshot{},
	})
}

// currentRecord returns the record of the most recently started turn when it
// belongs to the current generation; nil otherwise. The caller holds s.jmu.
func (s *Session) currentRecord() *turnRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.journal) == 0 {
		return nil
	}
	rec := s.journal[len(s.journal)-1]
	if rec.gen != s.gen {
		return nil
	}
	return rec
}

// beforeWrite is called by the write tools before a file is modified. It
// records the file's state before the first write of the current turn.
// s.jmu is held across lookup, read and insert so that a concurrent writer of
// the same file cannot modify it before its first snapshot is taken.
func (s *Session) beforeWrite(path string) {
	if _, ok := s.undoTarget(path); !ok {
		return
	}
	s.jmu.Lock()
	defer s.jmu.Unlock()
	rec := s.currentRecord()
	if rec == nil {
		return
	}
	if _, ok := rec.snaps[path]; ok {
		return
	}
	rec.snaps[path] = takeSnapshot(path)
}

// takeSnapshot reads the current state of path.
func takeSnapshot(path string) fileSnapshot {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return fileSnapshot{existed: false}
	}
	if err != nil || !fi.Mode().IsRegular() {
		return fileSnapshot{existed: true, unreadable: true}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fileSnapshot{existed: true, unreadable: true}
	}
	return fileSnapshot{existed: true, data: data, mode: fi.Mode().Perm()}
}

// recordBash appends the command of a bash call that ran in generation gen
// to the current turn.
func (s *Session) recordBash(gen uint64, args string) {
	var a struct {
		Command *string `json:"command"`
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil || a.Command == nil || strings.TrimSpace(*a.Command) == "" {
		return
	}
	s.jmu.Lock()
	defer s.jmu.Unlock()
	if rec := s.currentRecord(); rec != nil && rec.gen == gen {
		rec.bash = append(rec.bash, *a.Command)
	}
}

// Undo reverts the last turn: files written by write_file/edit_file (also by
// subagents) are restored to their state before the turn, files created in
// the turn are removed and the history is truncated to before the turn's
// user message. Bash commands are not reverted; they are listed. It returns
// ErrBusy while a turn runs and ErrNothingToUndo when no turn is left.
func (s *Session) Undo() (UndoResult, error) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return UndoResult{}, ErrBusy
	}
	if len(s.journal) == 0 {
		s.mu.Unlock()
		return UndoResult{}, ErrNothingToUndo
	}
	rec := s.journal[len(s.journal)-1]
	s.journal[len(s.journal)-1] = nil
	s.journal = s.journal[:len(s.journal)-1]
	if n := rec.histLen; n >= 1 && n <= len(s.history) {
		s.history = append([]provider.Message(nil), s.history[:n]...)
	}
	// Keep Submit out while the files are restored.
	s.busy = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.busy = false
		s.mu.Unlock()
	}()

	s.jmu.Lock()
	snaps := make(map[string]fileSnapshot, len(rec.snaps))
	for p, sn := range rec.snaps {
		snaps[p] = sn
	}
	res := UndoResult{BashNotReverted: append([]string(nil), rec.bash...)}
	s.jmu.Unlock()

	paths := make([]string, 0, len(snaps))
	for p := range snaps {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		rel, ok := s.undoTarget(p)
		if !ok {
			continue
		}
		sn := snaps[p]
		switch {
		case sn.unreadable:
		case sn.existed:
			if restoreFile(p, sn) {
				res.Restored = append(res.Restored, rel)
			}
		default:
			if removeCreated(p) {
				res.Deleted = append(res.Deleted, rel)
			}
		}
	}
	sort.Strings(res.Restored)
	sort.Strings(res.Deleted)
	return res, nil
}

// undoTarget re-checks a journal path with the path guard (defence in
// depth): it must resolve to itself (no symlink in its chain now) and lie
// strictly inside the start directory. It returns the "/"-separated path
// relative to the start directory.
func (s *Session) undoTarget(p string) (string, bool) {
	guard := s.registry.Guard()
	if !filepath.IsAbs(p) {
		return "", false
	}
	resolved, err := guard.Resolve(p)
	if err != nil || resolved != filepath.Clean(p) {
		return "", false
	}
	rel, err := filepath.Rel(guard.Root(), resolved)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// restoreFile writes the snapshot's bytes back to p and restores its mode.
// A path that now is a directory (or anything but a regular file) is skipped.
func restoreFile(p string, sn fileSnapshot) bool {
	fi, err := os.Lstat(p)
	switch {
	case err == nil:
		if !fi.Mode().IsRegular() {
			return false
		}
		if fi.Mode().Perm()&0o200 == 0 {
			_ = os.Chmod(p, fi.Mode().Perm()|0o200)
		}
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return false
		}
	default:
		return false
	}
	if err := os.WriteFile(p, sn.data, sn.mode); err != nil {
		return false
	}
	return os.Chmod(p, sn.mode) == nil
}

// removeCreated removes the file p created in the undone turn (only the
// file, never a directory). A file that is already gone counts as removed.
func removeCreated(p string) bool {
	fi, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	if err != nil || fi.IsDir() {
		return false
	}
	return os.Remove(p) == nil
}
