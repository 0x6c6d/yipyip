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
	"sort"
	"strings"
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
	Raw          map[string]any // the merged JSON object, after {env:NAME} substitution

	Providers map[string]Provider // keyed by provider id
	EnvNames  []string            // sorted unique NAMEs referenced via {env:NAME}
	Redactor  *Redactor           // never nil after Load; holds every resolved {env:} value

	Jev    *Jev    // "jev", nil when absent
	Search *Search // "search", nil when absent
}

// Provider is one entry of the "provider" object.
type Provider struct {
	ID      string           // provider id (the key under "provider")
	Name    string           // "name" or ""
	BaseURL string           // "options.baseURL" without trailing "/"
	APIKey  string           // "options.apiKey"
	Models  map[string]Model // keyed by model key
}

// Model is one entry of a provider's "models" object.
type Model struct {
	ID           string // "<provider id>/<model key>"
	Provider     string // provider id
	Key          string // model key, sent to the API as "model"
	Name         string // "name" or ""
	Reasoning    bool   // "reasoning", default true
	ContextLimit int    // "limit.context", 0 = unknown
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
// merges them (project wins), substitutes {env:NAME} references in every
// string value with getenv(NAME) (os.Getenv when getenv is nil) and applies
// defaults. A missing file counts as an empty object. An unset or empty
// variable yields a *MissingEnvError. Every resolved value is registered with
// the returned Config's Redactor. Errors never contain resolved values or
// config content. Load never writes to disk.
func Load(home, startDir string, getenv func(string) string) (*Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	global, err := readObject(GlobalPath(home))
	if err != nil {
		return nil, err
	}
	project, err := readObject(filepath.Join(startDir, ProjectFile))
	if err != nil {
		return nil, err
	}
	merged := Merge(global, project)

	redactor := NewRedactor()
	er := newEnvResolver(getenv, redactor)
	sub, err := er.substitute(merged)
	if err != nil {
		return nil, err
	}
	raw := sub.(map[string]any)

	cfg := &Config{
		MaxSubagents: defaultMaxSubagents,
		AutoCompact:  defaultAutoCompact,
		Raw:          raw,
		Providers:    map[string]Provider{},
		EnvNames:     er.envNames(),
		Redactor:     redactor,
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
	if v, ok := c.Raw["provider"]; ok {
		if err := c.fillProviders(v); err != nil {
			return err
		}
	}
	if v, ok := c.Raw["jev"]; ok {
		if err := c.fillJev(v); err != nil {
			return err
		}
	}
	if v, ok := c.Raw["search"]; ok {
		if err := c.fillSearch(v); err != nil {
			return err
		}
	}
	return nil
}

// typeError reports a wrongly typed field by its JSON path, never its value.
func typeError(path []string, want string) error {
	return fmt.Errorf("config: %q must be %s", strings.Join(path, "."), want)
}

func asObject(v any, path []string) (map[string]any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, typeError(path, "an object")
	}
	return m, nil
}

// optString returns m[key] as a string ("" when absent).
func optString(m map[string]any, key string, path []string) (string, error) {
	v, ok := m[key]
	if !ok {
		return "", nil
	}
	s, isStr := v.(string)
	if !isStr {
		return "", typeError(append(path, key), "a string")
	}
	return s, nil
}

func (c *Config) fillProviders(v any) error {
	path := []string{"provider"}
	provs, err := asObject(v, path)
	if err != nil {
		return err
	}
	for _, id := range sortedKeys(provs) {
		pv := provs[id]
		ppath := []string{"provider", id}
		if id == "" || strings.Contains(id, "/") {
			return errors.New(`config: provider ids under "provider" must be non-empty and must not contain "/"`)
		}
		pm, err := asObject(pv, ppath)
		if err != nil {
			return err
		}
		p := Provider{ID: id, Models: map[string]Model{}}
		if p.Name, err = optString(pm, "name", ppath); err != nil {
			return err
		}
		if ov, ok := pm["options"]; ok {
			opath := append(ppath[:len(ppath):len(ppath)], "options")
			om, err := asObject(ov, opath)
			if err != nil {
				return err
			}
			base, err := optString(om, "baseURL", opath)
			if err != nil {
				return err
			}
			p.BaseURL = strings.TrimRight(base, "/")
			if p.APIKey, err = optString(om, "apiKey", opath); err != nil {
				return err
			}
		}
		if mv, ok := pm["models"]; ok {
			mpath := append(ppath[:len(ppath):len(ppath)], "models")
			models, err := asObject(mv, mpath)
			if err != nil {
				return err
			}
			for _, key := range sortedKeys(models) {
				m := models[key]
				if key == "" {
					return typeError(mpath, "an object without empty model keys")
				}
				model, err := parseModel(id, key, m, append(mpath[:len(mpath):len(mpath)], key))
				if err != nil {
					return err
				}
				p.Models[key] = model
			}
		}
		c.Providers[id] = p
	}
	return nil
}

// sortedKeys returns the keys of m in sorted order, so the first error
// reported is deterministic.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func parseModel(provider, key string, v any, path []string) (Model, error) {
	m, err := asObject(v, path)
	if err != nil {
		return Model{}, err
	}
	model := Model{ID: provider + "/" + key, Provider: provider, Key: key, Reasoning: true}
	if model.Name, err = optString(m, "name", path); err != nil {
		return Model{}, err
	}
	if rv, ok := m["reasoning"]; ok {
		b, isBool := rv.(bool)
		if !isBool {
			return Model{}, typeError(append(path, "reasoning"), "a boolean")
		}
		model.Reasoning = b
	}
	if lv, ok := m["limit"]; ok {
		lpath := append(path[:len(path):len(path)], "limit")
		lm, err := asObject(lv, lpath)
		if err != nil {
			return Model{}, err
		}
		if cv, ok := lm["context"]; ok {
			f, isNum := cv.(float64)
			if !isNum || f != math.Trunc(f) || f < 0 || f > math.MaxInt32 {
				return Model{}, typeError(append(lpath, "context"), "a non-negative integer")
			}
			model.ContextLimit = int(f)
		}
	}
	return model, nil
}

// ModelIDs returns all model ids ("<provider id>/<model key>"), sorted.
func (c *Config) ModelIDs() []string {
	var ids []string
	for _, p := range c.Providers {
		for _, m := range p.Models {
			ids = append(ids, m.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

// Lookup returns the provider and model for a model id.
func (c *Config) Lookup(id string) (Provider, Model, bool) {
	pid, key, ok := strings.Cut(id, "/")
	if !ok {
		return Provider{}, Model{}, false
	}
	p, ok := c.Providers[pid]
	if !ok {
		return Provider{}, Model{}, false
	}
	m, ok := p.Models[key]
	if !ok {
		return Provider{}, Model{}, false
	}
	return p, m, true
}

// DefaultModelID returns default_model when it names a known model, else the
// first of ModelIDs, else "".
func (c *Config) DefaultModelID() string {
	if _, _, ok := c.Lookup(c.DefaultModel); ok {
		return c.DefaultModel
	}
	if ids := c.ModelIDs(); len(ids) > 0 {
		return ids[0]
	}
	return ""
}

// BaseURLs returns the sorted unique non-empty provider base URLs.
func (c *Config) BaseURLs() []string {
	set := map[string]struct{}{}
	for _, p := range c.Providers {
		if p.BaseURL != "" {
			set[p.BaseURL] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for u := range set {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}
