// Package crashlog appends redacted crash records to <startdir>/.yipyip/crash.log.
//
// A record holds only the version, the time, where a panic was recovered, the
// active session id, the redacted panic value and the redacted stack: never
// history, prompts, tool arguments or tool output. The package imports only the
// standard library so that any yipyip package can use it without an import cycle.
package crashlog

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

const (
	MaxBytes  = 1 << 20   // the log is cut when it grows past this
	KeepBytes = 512 << 10 // ... down to at most this
)

// logRel is the log path relative to the start directory.
const logRel = ".yipyip/crash.log"

// recordHead starts every record; endLine ends it.
const (
	recordHead = "=== yip crash "
	endLine    = "=== end ===\n"
)

// Options configures a Logger.
type Options struct {
	Version   string                           // the `yip --version` string
	Redact    func(string) string              // applied to the panic value and the stack; nil = identity
	Resolve   func(rel string) (string, error) // maps a path relative to the start dir to an absolute, confined path; required
	SessionID func() string                    // optional: id of the active tab's saved session, "" = none known
}

// Logger writes crash records. A nil *Logger is valid and does nothing.
type Logger struct {
	o Options
}

// writeMu serialises every append and cap rewrite of any Logger.
var writeMu sync.Mutex

// New returns a Logger for o.
func New(o Options) *Logger {
	return &Logger{o: o}
}

// Path returns the absolute, confined path of crash.log.
func (l *Logger) Path() (string, error) {
	if l == nil {
		return "", nil
	}
	if l.o.Resolve == nil {
		return "", errors.New("crashlog: no Resolve configured")
	}
	return l.o.Resolve(logRel)
}

// Fatal appends a "fatal" record with the stack of the calling goroutine and
// returns the log path. On any error the record goes to os.Stderr instead and
// the error is returned.
func (l *Logger) Fatal(v any) (path string, err error) {
	if l == nil {
		return "", nil
	}
	rec := l.record("fatal", "", v, debug.Stack())
	return l.write(rec)
}

// Recovered appends a "recovered" record; call it inside a deferred func right
// after recover(). It never panics and never returns an error: on failure the
// record goes to os.Stderr.
func (l *Logger) Recovered(where string, v any) {
	if l == nil {
		return
	}
	defer func() { _ = recover() }()
	rec := l.record("recovered", where, v, debug.Stack())
	_, _ = l.write(rec)
}

// redact applies Options.Redact, or returns s unchanged when it is nil.
func (l *Logger) redact(s string) string {
	if l.o.Redact == nil {
		return s
	}
	return l.o.Redact(s)
}

// record builds one complete, redacted record.
func (l *Logger) record(kind, where string, v any, stack []byte) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s %s ===\n", recordHead, time.Now().UTC().Format(time.RFC3339), kind)
	fmt.Fprintf(&b, "version: %s\n", oneLine(l.redact(l.o.Version)))
	if kind == "recovered" {
		fmt.Fprintf(&b, "where: %s\n", oneLine(l.redact(where)))
	}
	if l.o.SessionID != nil {
		if id := l.o.SessionID(); id != "" {
			fmt.Fprintf(&b, "session: %s\n", oneLine(l.redact(id)))
		}
	}
	fmt.Fprintf(&b, "panic: %s\n", noHeaders(l.redact(fmt.Sprint(v))))
	b.WriteString("stack:\n")
	st := noHeaders(l.redact(string(stack)))
	b.WriteString(st)
	if !strings.HasSuffix(st, "\n") {
		b.WriteString("\n")
	}
	b.WriteString(endLine)
	return []byte(b.String())
}

// oneLine replaces line breaks so a header field stays on its line.
func oneLine(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}

// noHeaders indents any line of s that would otherwise look like a record
// header or end marker, so record boundaries stay unambiguous.
func noHeaders(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if strings.HasPrefix(ln, "===") {
			lines[i] = " " + ln
		}
	}
	return strings.Join(lines, "\n")
}

// write appends rec to the log and applies the cap; on error rec goes to os.Stderr.
func (l *Logger) write(rec []byte) (string, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	path, err := l.appendRecord(rec)
	if err != nil {
		_, _ = os.Stderr.Write(rec)
		return "", err
	}
	return path, nil
}

// appendRecord resolves the path, appends rec in one Write and caps the file.
func (l *Logger) appendRecord(rec []byte) (string, error) {
	path, err := l.Path()
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", errors.New("crashlog: empty log path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	_, werr := f.Write(rec)
	cerr := f.Close()
	if werr != nil {
		return "", werr
	}
	if cerr != nil {
		return "", cerr
	}
	if err := capFile(path); err != nil {
		return "", err
	}
	return path, nil
}

// capFile cuts the log to at most KeepBytes once it is larger than MaxBytes.
func capFile(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.Size() <= MaxBytes {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return atomicWrite(path, cut(data))
}

// cut drops the oldest whole records of data until it is at most KeepBytes. If
// the newest record alone is larger, its head is kept up to a line boundary and
// closed with the end marker.
func cut(data []byte) []byte {
	starts := recordStarts(data)
	if len(starts) == 0 {
		starts = []int{0}
	}
	for _, s := range starts {
		if len(data)-s <= KeepBytes {
			return data[s:]
		}
	}
	last := data[starts[len(starts)-1]:]
	limit := KeepBytes - len(endLine)
	head := last[:limit]
	if i := bytes.LastIndexByte(head, '\n'); i >= 0 {
		head = head[:i+1]
	} else {
		head = nil
	}
	out := make([]byte, 0, len(head)+len(endLine))
	out = append(out, head...)
	return append(out, endLine...)
}

// recordStarts returns the offsets of every line that begins a record.
func recordStarts(data []byte) []int {
	var out []int
	head := []byte(recordHead)
	for i := 0; i < len(data); {
		if bytes.HasPrefix(data[i:], head) {
			out = append(out, i)
		}
		j := bytes.IndexByte(data[i:], '\n')
		if j < 0 {
			break
		}
		i += j + 1
	}
	return out
}

// atomicWrite replaces path with data through a 0600 temp file in the same directory.
func atomicWrite(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".crash-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	return nil
}

// defaultMu guards def.
var (
	defaultMu sync.RWMutex
	def       *Logger
)

// SetDefault sets the package default Logger; nil clears it.
func SetDefault(l *Logger) {
	defaultMu.Lock()
	def = l
	defaultMu.Unlock()
}

// Default returns the package default Logger, or nil while none is set.
func Default() *Logger {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return def
}

// Recovered records a recovered panic with the default Logger; it is a no-op
// while no default is set.
func Recovered(where string, v any) {
	Default().Recovered(where, v)
}
