package config

import (
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
)

// Defaults for the "jev" block.
const (
	DefaultJevBaseURL       = "https://api.typesafe.ai"
	DefaultJevModel         = "jev-latest"
	DefaultJevMinConfidence = 0.7
)

// Jev is the "jev" object. Config.Jev is nil when the key "jev" is absent.
type Jev struct {
	BaseURL       string    // "baseURL" without trailing "/", default DefaultJevBaseURL
	APIKey        string    // "apiKey", "" when absent (normally "{env:TYPESAFE_API_KEY}")
	Model         string    // "model", default DefaultJevModel
	MinConfidence float64   // "min_confidence", default DefaultJevMinConfidence
	Route         *JevRoute // nil when "route" is absent
}

// JevRoute is "jev.route": model ids "<provider>/<model>" for easy and hard turns.
type JevRoute struct{ Easy, Hard string }

// fillJev parses the "jev" object. It must run after fillProviders, since
// route entries are checked against the configured models. Errors name JSON
// paths only, never values.
func (c *Config) fillJev(v any) error {
	path := []string{"jev"}
	m, err := asObject(v, path)
	if err != nil {
		return err
	}
	j := &Jev{
		BaseURL:       DefaultJevBaseURL,
		Model:         DefaultJevModel,
		MinConfidence: DefaultJevMinConfidence,
	}
	if _, ok := m["baseURL"]; ok {
		base, err := optString(m, "baseURL", path)
		if err != nil {
			return err
		}
		if !validJevURL(base) {
			return typeError(append(path, "baseURL"), "an absolute http or https URL")
		}
		j.BaseURL = strings.TrimRight(base, "/")
	}
	if j.APIKey, err = optString(m, "apiKey", path); err != nil {
		return err
	}
	if _, ok := m["model"]; ok {
		if j.Model, err = optString(m, "model", path); err != nil {
			return err
		}
	}
	if cv, ok := m["min_confidence"]; ok {
		f, isNum := cv.(float64)
		if !isNum || f < 0 || f > 1 {
			return typeError(append(path, "min_confidence"), "a number between 0 and 1")
		}
		j.MinConfidence = f
	}
	if rv, ok := m["route"]; ok {
		if j.Route, err = c.parseJevRoute(rv, append(path, "route")); err != nil {
			return err
		}
	}
	c.Jev = j
	return nil
}

// AllowedBaseURLs returns the base URLs the HTTP transport may reach: the
// provider base URLs plus Jev.BaseURL while "jev" is configured, sorted and
// unique. Without "jev" it equals BaseURLs.
func (c *Config) AllowedBaseURLs() []string {
	out := c.BaseURLs()
	if c.Jev == nil || c.Jev.BaseURL == "" {
		return out
	}
	i := sort.SearchStrings(out, c.Jev.BaseURL)
	if i < len(out) && out[i] == c.Jev.BaseURL {
		return out
	}
	return slices.Insert(out, i, c.Jev.BaseURL)
}

// validJevURL reports whether s is an absolute http(s) URL with a host.
func validJevURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func (c *Config) parseJevRoute(v any, path []string) (*JevRoute, error) {
	m, err := asObject(v, path)
	if err != nil {
		return nil, err
	}
	r := &JevRoute{}
	for _, f := range []struct {
		key string
		dst *string
	}{{"easy", &r.Easy}, {"hard", &r.Hard}} {
		kpath := strings.Join(append(path[:len(path):len(path)], f.key), ".")
		mv, ok := m[f.key]
		if !ok {
			return nil, fmt.Errorf("config: %q is required", kpath)
		}
		id, isStr := mv.(string)
		if !isStr {
			return nil, fmt.Errorf("config: %q must be a string", kpath)
		}
		if _, _, known := c.Lookup(id); !known {
			return nil, fmt.Errorf("config: %q names an unknown model", kpath)
		}
		*f.dst = id
	}
	return r, nil
}
