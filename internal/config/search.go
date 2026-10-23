package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// DefaultSearchBaseURL is the default "search.baseURL".
const DefaultSearchBaseURL = "https://api.search.brave.com"

// Search is the "search" object. Config.Search is nil when the key "search"
// is absent.
type Search struct {
	BaseURL string // "baseURL" without trailing "/", default DefaultSearchBaseURL
	APIKey  string // "apiKey", the resolved key (required, never empty)
}

// searchURLError is the error for an invalid "search.baseURL". It names the
// JSON path only, never the given value.
const searchURLError = `config: "search.baseURL" must be an https URL (http only for 127.0.0.1, ::1 or localhost)`

// fillSearch parses the "search" object. Errors name JSON paths only, never
// values. The key is registered with the Redactor so a literal (non-{env:})
// key is redacted like an {env:} value. The search base URL is deliberately
// not part of BaseURLs: the search client builds its own transport.
func (c *Config) fillSearch(v any) error {
	path := []string{"search"}
	m, err := asObject(v, path)
	if err != nil {
		return err
	}
	s := &Search{BaseURL: DefaultSearchBaseURL}

	kv, ok := m["apiKey"]
	if !ok {
		return fmt.Errorf("config: %q is required", "search.apiKey")
	}
	key, isStr := kv.(string)
	if !isStr || key == "" {
		return typeError(append(path, "apiKey"), "a non-empty string")
	}
	c.Redactor.Add(key)
	s.APIKey = key

	if bv, ok := m["baseURL"]; ok {
		base, isStr := bv.(string)
		if !isStr || !validSearchURL(base) {
			return errors.New(searchURLError)
		}
		s.BaseURL = strings.TrimRight(base, "/")
	}
	c.Search = s
	return nil
}

// validSearchURL reports whether s is an absolute https URL with a host, or
// an http URL whose host (without port, case-insensitive) is exactly
// 127.0.0.1, ::1 or localhost.
func validSearchURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return true
	case "http":
		switch strings.ToLower(u.Hostname()) {
		case "127.0.0.1", "::1", "localhost":
			return true
		}
	}
	return false
}
