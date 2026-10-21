package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	maxSearchResults = 1000
	maxGrepLine      = 1 << 20 // longer lines are cut, not dropped
	binarySniffBytes = 8000

	globSchema = `{"type":"object","properties":{"pattern":{"type":"string"},"path":{"type":"string"}},"required":["pattern"]}`
	grepSchema = `{"type":"object","properties":{"pattern":{"type":"string"},"path":{"type":"string"},"glob":{"type":"string"}},"required":["pattern"]}`
)

func (r *Registry) registerSearchTools() {
	r.register(Tool{
		Name: "glob",
		Description: "List files inside the start directory whose path (relative to the search path, default \".\") " +
			"matches a glob pattern. Supports *, ?, [...] within one path segment and ** as a whole segment " +
			"matching any number of directories, e.g. \"**/*.go\". Skips .git. Returns sorted paths relative to the start directory.",
		Parameters: json.RawMessage(globSchema),
		Gated:      false,
	}, r.runGlob)
	r.register(Tool{
		Name: "grep",
		Description: "Search file contents with a Go (RE2) regular expression. Optional path (file or directory, " +
			"default the start directory) and glob filter (a pattern with / is matched against the path relative to the " +
			"start directory, otherwise against the file name). Skips .git and binary files. " +
			"Output lines are path:line:text.",
		Parameters: json.RawMessage(grepSchema),
		Gated:      false,
	}, r.runGrep)
}

// MatchGlob reports whether the slash-separated relative path matches the
// pattern. *, ? and [...] work within one segment (as path.Match); a "**"
// segment matches zero or more segments. A malformed pattern never matches.
func MatchGlob(pattern, path string) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return matchSegments(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

func matchSegments(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for len(pat) > 1 && pat[1] == "**" {
				pat = pat[1:]
			}
			rest := pat[1:]
			if len(rest) == 0 {
				return true
			}
			for i := 0; i <= len(segs); i++ {
				if matchSegments(rest, segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		m, err := pathpkg.Match(pat[0], segs[0])
		if err != nil || !m {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}

// searchTarget resolves the optional "path" argument.
func (r *Registry) searchTarget(obj map[string]json.RawMessage) (abs string, isDir bool, res *Result) {
	p := "."
	if raw, ok := obj["path"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			e := errResult("invalid arguments: argument %q must be a string", "path")
			return "", false, &e
		}
		if s != "" {
			p = s
		}
	}
	abs, err := r.guard.Resolve(p)
	if err != nil {
		e := errResult("%v", err)
		return "", false, &e
	}
	fi, err := os.Stat(abs)
	if err != nil {
		e := errResult("cannot search %s: %v", p, err)
		return "", false, &e
	}
	return abs, fi.IsDir(), nil
}

// walkFiles calls fn for every regular file below root (not following
// symlinks, skipping .git directories). rel is relative to root.
func walkFiles(ctx context.Context, root string, fn func(abs, rel string)) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil // unreadable entry: skip
		}
		if d.IsDir() {
			if d.Name() == ".git" && p != root {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		fn(p, filepath.ToSlash(rel))
		return nil
	})
}

func (r *Registry) runGlob(ctx context.Context, args json.RawMessage) Result {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(args, &obj); err != nil {
		return errResult("invalid arguments: %v", err)
	}
	pattern, err := stringArg(obj, "pattern")
	if err != nil {
		return errResult("invalid arguments: %v", err)
	}
	if pattern == "" {
		return errResult("invalid arguments: argument %q is empty", "pattern")
	}
	abs, isDir, bad := r.searchTarget(obj)
	if bad != nil {
		return *bad
	}
	if !isDir {
		return Result{Output: "no matches"}
	}
	var found []string
	err = walkFiles(ctx, abs, func(p, rel string) {
		if MatchGlob(pattern, rel) {
			found = append(found, r.relPath(p))
		}
	})
	if err != nil {
		return errResult("search failed: %v", err)
	}
	if len(found) == 0 {
		return Result{Output: "no matches"}
	}
	sort.Strings(found)
	var extra int
	if len(found) > maxSearchResults {
		extra = len(found) - maxSearchResults
		found = found[:maxSearchResults]
	}
	out := strings.Join(found, "\n")
	if extra > 0 {
		out += fmt.Sprintf("\n[results truncated: %d more]", extra)
	}
	return Result{Output: Truncate(out)}
}

type grepHit struct {
	path string
	line int
	text string
}

func (r *Registry) runGrep(ctx context.Context, args json.RawMessage) Result {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(args, &obj); err != nil {
		return errResult("invalid arguments: %v", err)
	}
	pattern, err := stringArg(obj, "pattern")
	if err != nil {
		return errResult("invalid arguments: %v", err)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return errResult("invalid regular expression: %v", err)
	}
	var globPat string
	if raw, ok := obj["glob"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if err := json.Unmarshal(raw, &globPat); err != nil {
			return errResult("invalid arguments: argument %q must be a string", "glob")
		}
	}
	abs, isDir, bad := r.searchTarget(obj)
	if bad != nil {
		return *bad
	}

	var hits []grepHit
	visit := func(p string) {
		rel := r.relPath(p)
		if globPat != "" {
			if strings.Contains(globPat, "/") {
				if !MatchGlob(globPat, rel) {
					return
				}
			} else if m, _ := pathpkg.Match(globPat, pathpkg.Base(rel)); !m {
				return
			}
		}
		grepFile(p, rel, re, &hits)
	}
	if isDir {
		err = walkFiles(ctx, abs, func(p, _ string) { visit(p) })
		if err != nil {
			return errResult("search failed: %v", err)
		}
	} else {
		visit(abs)
	}
	if len(hits) == 0 {
		return Result{Output: "no matches"}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].path != hits[j].path {
			return hits[i].path < hits[j].path
		}
		return hits[i].line < hits[j].line
	})
	var extra int
	if len(hits) > maxSearchResults {
		extra = len(hits) - maxSearchResults
		hits = hits[:maxSearchResults]
	}
	var b strings.Builder
	for i, h := range hits {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(h.path + ":" + strconv.Itoa(h.line) + ":" + h.text)
	}
	if extra > 0 {
		fmt.Fprintf(&b, "\n[results truncated: %d more matches]", extra)
	}
	return Result{Output: Truncate(b.String())}
}

// grepFile appends the matching lines of one file; binary and unreadable
// files are skipped.
func grepFile(abs, rel string, re *regexp.Regexp, hits *[]grepHit) {
	f, err := os.Open(abs)
	if err != nil {
		return
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 64*1024)
	head, _ := br.Peek(binarySniffBytes)
	if bytes.IndexByte(head, 0) >= 0 {
		return
	}
	for n := 1; ; n++ {
		line, err := readCappedLine(br)
		if len(line) > 0 || err == nil {
			text := strings.TrimSuffix(string(line), "\r")
			if re.MatchString(text) {
				*hits = append(*hits, grepHit{path: rel, line: n, text: text})
			}
		}
		if err != nil {
			return
		}
	}
}

// readCappedLine reads one line without its newline; bytes beyond
// maxGrepLine are discarded. err is io.EOF (or another error) after the last line.
func readCappedLine(br *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := br.ReadSlice('\n')
		if room := maxGrepLine - len(line); room > 0 {
			c := chunk
			if len(c) > room {
				c = c[:room]
			}
			line = append(line, c...)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err == nil {
			return bytes.TrimSuffix(line, []byte("\n")), nil
		}
		if err == io.EOF {
			return line, io.EOF
		}
		return line, err
	}
}
