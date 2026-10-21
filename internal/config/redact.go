package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Redacted is the text that replaces every occurrence of a secret.
const Redacted = "[redacted]"

// Redactor masks registered secret values in text. It is the only place in
// yipyip where secret masking is implemented; every other package calls it.
//
// A nil *Redactor is valid and redacts nothing. A Redactor is safe for
// concurrent use. It never prints its secrets: formatting it with any fmt
// verb yields only the number of registered secrets.
type Redactor struct {
	mu sync.Mutex // serialises Add; Redact is lock-free
	// state is replaced copy-on-write by Add. Keeping it behind a pointer
	// also means that printing a Redactor value by reflection shows only an
	// address, never the secrets.
	state atomic.Pointer[redactState]
}

type redactState struct {
	secrets  map[string]struct{} // registered raw secrets (deduplicated)
	patterns []string            // raw + escaped forms, unique, longest first
}

// NewRedactor returns a Redactor for the given secrets; empty strings are ignored.
func NewRedactor(secrets ...string) *Redactor {
	r := &Redactor{}
	for _, s := range secrets {
		r.Add(s)
	}
	return r
}

// Add registers a secret. Empty secrets and duplicates are ignored, as is
// any call on a nil Redactor.
func (r *Redactor) Add(secret string) {
	if r == nil || secret == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	old := r.state.Load()
	if old != nil {
		if _, dup := old.secrets[secret]; dup {
			return
		}
	}
	next := &redactState{secrets: make(map[string]struct{}, 1)}
	if old != nil {
		for s := range old.secrets {
			next.secrets[s] = struct{}{}
		}
	}
	next.secrets[secret] = struct{}{}
	next.patterns = buildPatterns(next.secrets)
	r.state.Store(next)
}

// Redact returns s with every occurrence of every registered secret (and of
// its JSON-escaped forms) replaced by Redacted.
//
// All occurrences of all patterns are located in the original text first and
// overlapping matches are merged into one span before replacing, so a secret
// that contains, or partially overlaps, another one leaves no fragment, and
// the inserted Redacted text is never re-scanned.
func (r *Redactor) Redact(s string) string {
	if r == nil || s == "" {
		return s
	}
	st := r.state.Load()
	if st == nil || len(st.patterns) == 0 {
		return s
	}
	return replaceSpans(s, findSpans(s, st.patterns))
}

// findSpans returns every occurrence of every pattern in s, sorted by start
// (longest first on ties).
func findSpans(s string, patterns []string) []span {
	var spans []span
	for _, p := range patterns {
		// Advance by one byte after each match so overlapping occurrences
		// of the same pattern (e.g. "aa" in "aaa") are all covered.
		for off := 0; off < len(s); {
			i := strings.Index(s[off:], p)
			if i < 0 {
				break
			}
			start := off + i
			spans = append(spans, span{start, start + len(p)})
			off = start + 1
		}
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].end > spans[j].end
	})
	return spans
}

// replaceSpans replaces the (sorted) spans of s by Redacted, merging
// overlapping spans into one.
func replaceSpans(s string, spans []span) string {
	if len(spans) == 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	pos := 0
	cur := spans[0]
	flush := func(sp span) {
		b.WriteString(s[pos:sp.start])
		b.WriteString(Redacted)
		pos = sp.end
	}
	for _, sp := range spans[1:] {
		if sp.start < cur.end { // overlapping: merge (adjacent spans stay separate)
			if sp.end > cur.end {
				cur.end = sp.end
			}
			continue
		}
		flush(cur)
		cur = sp
	}
	flush(cur)
	b.WriteString(s[pos:])
	return b.String()
}

// StreamRedactor redacts text that arrives in pieces (for example streamed
// text deltas), so that a secret split across pieces is masked as well. Write
// returns the part of the text received so far that can no longer become part
// of a secret, already redacted; the rest is held back until more text
// arrives or Flush is called. The concatenation of all returned pieces equals
// Redact of the concatenated input. A StreamRedactor is not safe for
// concurrent use.
type StreamRedactor struct {
	r   *Redactor
	buf string
}

// Stream returns a StreamRedactor using r's secrets (nil-safe: a nil
// Redactor yields a StreamRedactor that passes text through unchanged).
func (r *Redactor) Stream() *StreamRedactor { return &StreamRedactor{r: r} }

// Write adds piece and returns the redacted text that is safe to release.
func (sr *StreamRedactor) Write(piece string) string {
	if sr == nil {
		return piece
	}
	var st *redactState
	if sr.r != nil {
		st = sr.r.state.Load()
	}
	if st == nil || len(st.patterns) == 0 {
		out := sr.buf + piece
		sr.buf = ""
		return out
	}
	buf := sr.buf + piece
	// cut starts at the earliest suffix that is a proper prefix of a pattern:
	// that text may still turn into a secret.
	cut := len(buf)
	for _, p := range st.patterns {
		for k := min(len(p)-1, len(buf)); k > 0 && len(buf)-k < cut; k-- {
			if strings.HasSuffix(buf, p[:k]) {
				cut = len(buf) - k
				break
			}
		}
	}
	// Never cut through a match (or a chain of overlapping matches): move
	// the cut back to its start so the released part redacts exactly as the
	// whole text would.
	spans := findSpans(buf, st.patterns)
	for moved := true; moved; {
		moved = false
		for _, sp := range spans {
			if sp.start < cut && cut < sp.end {
				cut = sp.start
				moved = true
			}
		}
	}
	sr.buf = buf[cut:]
	return replaceSpans(buf[:cut], findSpans(buf[:cut], st.patterns))
}

// Flush returns the redacted rest of the held-back text.
func (sr *StreamRedactor) Flush() string {
	if sr == nil {
		return ""
	}
	out := sr.r.Redact(sr.buf)
	sr.buf = ""
	return out
}

// Format implements fmt.Formatter so that no fmt verb can reveal a secret;
// it prints only the number of registered secrets.
func (r *Redactor) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprintf(f, "config.Redactor(%d secrets)", r.count())
}

// String reports only the number of registered secrets.
func (r *Redactor) String() string { return fmt.Sprintf("config.Redactor(%d secrets)", r.count()) }

// GoString reports only the number of registered secrets.
func (r *Redactor) GoString() string { return r.String() }

func (r *Redactor) count() int {
	if r == nil {
		return 0
	}
	if st := r.state.Load(); st != nil {
		return len(st.secrets)
	}
	return 0
}

type span struct{ start, end int }

// buildPatterns returns the unique raw and JSON-escaped forms of the secrets,
// longest first (ties broken lexically for determinism).
func buildPatterns(secrets map[string]struct{}) []string {
	set := make(map[string]struct{}, 3*len(secrets))
	for s := range secrets {
		set[s] = struct{}{}
		for _, e := range jsonEscaped(s) {
			if e != "" {
				set[e] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

// jsonEscaped returns the JSON string encodings of s without the surrounding
// quotes: as produced by json.Marshal (HTML-escaped) and by an Encoder with
// HTML escaping disabled. Forms equal to s are included harmlessly; the
// caller deduplicates.
func jsonEscaped(s string) []string {
	var forms []string
	if b, err := json.Marshal(s); err == nil && len(b) >= 2 {
		forms = append(forms, string(b[1:len(b)-1]))
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err == nil {
		b := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
		if len(b) >= 2 {
			forms = append(forms, string(b[1:len(b)-1]))
		}
	}
	return forms
}
