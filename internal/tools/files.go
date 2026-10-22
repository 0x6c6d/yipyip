package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	readFileSchema  = `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`
	writeFileSchema = `{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`
	editFileSchema  = `{"type":"object","properties":{"path":{"type":"string"},"old_string":{"type":"string"},"new_string":{"type":"string"}},"required":["path","old_string","new_string"]}`
)

func (r *Registry) registerFileTools() {
	r.register(Tool{
		Name:        "read_file",
		Description: "Read a text file at any path (relative paths start at the start directory) and return its content (long output is truncated).",
		Parameters:  json.RawMessage(readFileSchema),
		Gated:       false,
	}, r.runReadFile)
	r.register(Tool{
		Name: "write_file",
		Description: "Write content to a file inside the start directory, creating it and missing parent " +
			"directories, or overwriting it.",
		Parameters: json.RawMessage(writeFileSchema),
		Gated:      true,
	}, r.runWriteFile)
	r.register(Tool{
		Name: "edit_file",
		Description: "Replace exactly one occurrence of old_string with new_string in an existing file. " +
			"Fails if old_string is empty, not found, or occurs more than once.",
		Parameters: json.RawMessage(editFileSchema),
		Gated:      true,
	}, r.runEditFile)
}

// relPath returns abs relative to the start directory with "/" separators.
func (r *Registry) relPath(abs string) string {
	rel, err := filepath.Rel(r.guard.Root(), abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}

// displayPath is the start-directory-relative slash path of abs when the
// guard accepts it as inside the start directory, else the absolute path.
func (r *Registry) displayPath(abs string) string {
	if _, err := r.guard.Resolve(abs); err != nil {
		return filepath.ToSlash(abs)
	}
	return r.relPath(abs)
}

// stringArgs parses args and extracts the named required string arguments.
func stringArgs(args json.RawMessage, keys ...string) ([]string, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(args, &obj); err != nil {
		return nil, err
	}
	out := make([]string, len(keys))
	for i, k := range keys {
		s, err := stringArg(obj, k)
		if err != nil {
			return nil, err
		}
		out[i] = s
	}
	return out, nil
}

func (r *Registry) runReadFile(_ context.Context, args json.RawMessage) Result {
	a, err := stringArgs(args, "path")
	if err != nil {
		return errResult("invalid arguments: %v", err)
	}
	abs, err := r.guard.ResolveRead(a[0])
	if err != nil {
		return errResult("%v", err)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return errResult("cannot read %s: %v", r.displayPath(abs), err)
	}
	if fi.IsDir() {
		return errResult("%s is a directory, not a file", r.displayPath(abs))
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return errResult("cannot read %s: %v", r.displayPath(abs), err)
	}
	return Result{Output: Truncate(string(data))}
}

func (r *Registry) runWriteFile(_ context.Context, args json.RawMessage) Result {
	a, err := stringArgs(args, "path", "content")
	if err != nil {
		return errResult("invalid arguments: %v", err)
	}
	content := a[1]
	abs, err := r.guard.Resolve(a[0])
	if err != nil {
		return errResult("%v", err)
	}
	rel := r.relPath(abs)
	mode := os.FileMode(0o644)
	existed := false
	if fi, err := os.Stat(abs); err == nil {
		if fi.IsDir() {
			return errResult("%s is a directory, not a file", rel)
		}
		mode = fi.Mode().Perm()
		existed = true
	} else if !os.IsNotExist(err) {
		return errResult("cannot write %s: %v", rel, err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return errResult("cannot create directories for %s: %v", rel, err)
	}
	if r.opts.BeforeWrite != nil {
		r.opts.BeforeWrite(abs)
	}
	if err := os.WriteFile(abs, []byte(content), mode); err != nil {
		return errResult("cannot write %s: %v", rel, err)
	}
	if !existed {
		_ = os.Chmod(abs, mode) // independent of the umask
	}
	return Result{Output: "wrote " + strconv.Itoa(len(content)) + " bytes to " + rel}
}

func (r *Registry) runEditFile(_ context.Context, args json.RawMessage) Result {
	a, err := stringArgs(args, "path", "old_string", "new_string")
	if err != nil {
		return errResult("invalid arguments: %v", err)
	}
	oldS, newS := a[1], a[2]
	abs, err := r.guard.Resolve(a[0])
	if err != nil {
		return errResult("%v", err)
	}
	rel := r.relPath(abs)
	if oldS == "" {
		return errResult("old_string must not be empty")
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return errResult("cannot edit %s: %v", rel, err)
	}
	if fi.IsDir() {
		return errResult("%s is a directory, not a file", rel)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return errResult("cannot read %s: %v", rel, err)
	}
	text := string(data)
	switch n := strings.Count(text, oldS); {
	case n == 0:
		return errResult("old_string not found in %s", rel)
	case n > 1:
		return errResult("old_string is ambiguous: %d occurrences in %s", n, rel)
	}
	updated := strings.Replace(text, oldS, newS, 1)
	if r.opts.BeforeWrite != nil {
		r.opts.BeforeWrite(abs)
	}
	if err := os.WriteFile(abs, []byte(updated), fi.Mode().Perm()); err != nil {
		return errResult("cannot write %s: %v", rel, err)
	}
	return Result{Output: "edited " + rel}
}
