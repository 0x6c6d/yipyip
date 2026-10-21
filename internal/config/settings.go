package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Top-level keys written by SaveSettings. The model key reuses the existing
// schema name "default_model" (read by Load into Config.DefaultModel), so a
// later start selects the saved model without any change to the load path.
// Effort and mode had no key in the schema before; they use the plain names
// "effort" and "mode" from the v3 spec.
const (
	SettingsKeyModel  = "default_model"
	SettingsKeyEffort = "effort"
	SettingsKeyMode   = "mode"
)

// settingsFileMode is the permission of the written config file (the mode
// os.CreateTemp gives the temp file, kept across the rename).
const settingsFileMode fs.FileMode = 0o600

// SaveSettings writes default model/effort/mode into the user config file at
// path (usually GlobalPath(home)). It returns the write error, or nil. It
// never writes any other key's resolved value and never writes a resolved
// {env:NAME} secret: those tokens stay literal.
//
// The file is read as raw JSON (no {env:NAME} substitution), so every other
// top-level value is carried over unchanged, including unknown keys,
// numbers and {env:NAME} tokens. Only the keys SettingsKeyModel,
// SettingsKeyEffort and SettingsKeyMode are set. A missing file (and parent
// directory, created 0700) is fine; an existing file that is not a JSON
// object is never overwritten. The new content is written to a temp file in
// the same directory (mode 0600), synced, closed and renamed over path, so
// the file is either the old or the complete new version. Output is indented
// JSON with sorted keys and a trailing newline. Every error is prefixed
// "could not save settings: ".
func SaveSettings(path string, model, effort, mode string) error {
	if err := saveSettings(path, model, effort, mode); err != nil {
		return fmt.Errorf("could not save settings: %w", err)
	}
	return nil
}

func saveSettings(path, model, effort, mode string) error {
	obj := map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &obj); err != nil {
			return fmt.Errorf("invalid JSON in %s: %w", path, err)
		}
		if obj == nil { // the file holds JSON null
			return fmt.Errorf("%s: top-level value must be a JSON object", path)
		}
	case errors.Is(err, fs.ErrNotExist):
		// Fresh file with only the three keys.
	default:
		return err
	}

	for k, v := range map[string]string{
		SettingsKeyModel:  model,
		SettingsKeyEffort: effort,
		SettingsKeyMode:   mode,
	} {
		b, err := marshalNoEscape(v)
		if err != nil {
			return err
		}
		obj[k] = b
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(obj); err != nil { // sorted keys, trailing newline
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return writeAtomic(path, buf.Bytes())
}

// marshalNoEscape encodes v as JSON without HTML escaping and without the
// trailing newline.
func marshalNoEscape(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return json.RawMessage(bytes.TrimSuffix(buf.Bytes(), []byte("\n"))), nil
}

// writeAtomic writes data to a temp file next to path and renames it over
// path. On any failure the temp file is removed and path is left untouched.
func writeAtomic(path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(name)
		}
	}()
	if err = tmp.Chmod(settingsFileMode); err != nil {
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
