package config

import (
	"regexp"
	"sort"
)

// envRef matches one {env:NAME} reference.
var envRef = regexp.MustCompile(`\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

// MissingEnvError reports a {env:NAME} reference whose variable is unset or
// empty. Its text names the variable only, never a value or config content.
type MissingEnvError struct{ Name string }

func (e *MissingEnvError) Error() string {
	return "environment variable " + e.Name + " is not set or empty"
}

// envResolver substitutes {env:NAME} references, caching each lookup so a
// variable is read once, and registers every resolved value as a secret.
type envResolver struct {
	getenv   func(string) string
	redactor *Redactor
	values   map[string]string
	names    map[string]struct{}
}

func newEnvResolver(getenv func(string) string, r *Redactor) *envResolver {
	return &envResolver{getenv: getenv, redactor: r, values: map[string]string{}, names: map[string]struct{}{}}
}

// substitute returns a copy of v with every {env:NAME} in every string value
// (at any depth) replaced. Object keys are never substituted. Objects are
// walked in sorted key order and arrays in index order, so the first missing
// variable reported is deterministic. Substituted text is not re-scanned.
func (er *envResolver) substitute(v any) (any, error) {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for _, k := range sortedKeys(t) {
			nv, err := er.substitute(t[k])
			if err != nil {
				return nil, err
			}
			out[k] = nv
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			nv, err := er.substitute(e)
			if err != nil {
				return nil, err
			}
			out[i] = nv
		}
		return out, nil
	case string:
		return er.substituteString(t)
	default:
		return v, nil
	}
}

func (er *envResolver) substituteString(s string) (string, error) {
	locs := envRef.FindAllStringSubmatchIndex(s, -1)
	if len(locs) == 0 {
		return s, nil
	}
	out := make([]byte, 0, len(s))
	pos := 0
	for _, loc := range locs {
		name := s[loc[2]:loc[3]]
		val, err := er.resolve(name)
		if err != nil {
			return "", err
		}
		out = append(out, s[pos:loc[0]]...)
		out = append(out, val...)
		pos = loc[1]
	}
	out = append(out, s[pos:]...)
	return string(out), nil
}

func (er *envResolver) resolve(name string) (string, error) {
	er.names[name] = struct{}{}
	if v, ok := er.values[name]; ok {
		return v, nil
	}
	v := er.getenv(name)
	if v == "" {
		return "", &MissingEnvError{Name: name}
	}
	er.values[name] = v
	er.redactor.Add(v)
	return v, nil
}

// envNames returns the sorted unique names referenced so far.
func (er *envResolver) envNames() []string {
	out := make([]string, 0, len(er.names))
	for n := range er.names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
