// Package config loads the yipyip configuration: a global file plus a
// project override, deep-merged, with defaults for the known keys.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
)

// ProjectFile is the name of the per-project override file in the start directory.
const ProjectFile = "yipyip.json"

const (
	defaultMaxSubagents = 4
	defaultAutoCompact  = true
)

// GlobalPath returns the path of the global config file under home.
func GlobalPath(home string) string {
	return filepath.Join(home, ".config", "yipyip", "config.json")
}

// Config is the loaded configuration.
type Config struct {
	DefaultModel string         // "default_model", "" when absent
	MaxSubagents int            // "max_subagents", default 4 when absent or <= 0
	AutoCompact  bool           // "auto_compact", default true when absent
	Raw          map[string]any // the merged JSON object
}

// Merge returns a new map holding base overlaid with override. When both
// values for a key are JSON objects they are merged recursively; any other
// override value replaces the base value. Neither input is modified and the
// result shares no maps or slices with them.
func Merge(base, override map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(override))
	for k, v := range base {
		out[k] = cloneValue(v)
	}
	for k, ov := range override {
		om, oIsMap := ov.(map[string]any)
		bm, bIsMap := out[k].(map[string]any)
		if oIsMap && bIsMap {
			out[k] = Merge(bm, om)
			continue
		}
		out[k] = cloneValue(ov)
	}
	return out
}

func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, e := range t {
			m[k] = cloneValue(e)
		}
		return m
	case []any:
		s := make([]any, len(t))
		for i, e := range t {
			s[i] = cloneValue(e)
		}
		return s
	default:
		return v
	}
}

// Load reads the global config under home and the project file in startDir,
// merges them (project wins) and applies defaults. A missing file counts as
// an empty object. getenv is reserved for later {env:NAME} substitution and
// may be nil. Load never writes to disk.
func Load(home, startDir string, getenv func(string) string) (*Config, error) {
	_ = getenv

	global, err := readObject(GlobalPath(home))
	if err != nil {
		return nil, err
	}
	project, err := readObject(filepath.Join(startDir, ProjectFile))
	if err != nil {
		return nil, err
	}
	raw := Merge(global, project)

	cfg := &Config{
		MaxSubagents: defaultMaxSubagents,
		AutoCompact:  defaultAutoCompact,
		Raw:          raw,
	}
	if err := cfg.fill(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// readObject reads path as a JSON object. A missing file yields an empty map.
// Errors name the path but never include the file contents.
func readObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("config: cannot read %s: %w", path, err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("config: invalid JSON in %s: %w", path, err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("config: %s: top-level value must be a JSON object", path)
	}
	return obj, nil
}

// fill extracts the known keys from Raw, validating their types.
func (c *Config) fill() error {
	if v, ok := c.Raw["default_model"]; ok {
		s, isStr := v.(string)
		if !isStr {
			return errors.New(`config: "default_model" must be a string`)
		}
		c.DefaultModel = s
	}
	if v, ok := c.Raw["max_subagents"]; ok {
		f, isNum := v.(float64)
		if !isNum || f != math.Trunc(f) || math.Abs(f) > math.MaxInt32 {
			return errors.New(`config: "max_subagents" must be an integer`)
		}
		if n := int(f); n > 0 {
			c.MaxSubagents = n
		}
	}
	if v, ok := c.Raw["auto_compact"]; ok {
		b, isBool := v.(bool)
		if !isBool {
			return errors.New(`config: "auto_compact" must be a boolean`)
		}
		c.AutoCompact = b
	}
	return nil
}
