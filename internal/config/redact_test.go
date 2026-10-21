package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestRedactorReplaces(t *testing.T) {
	r := NewRedactor("hunter2", "tok-42")
	cases := map[string]string{
		"":                             "",
		"nothing here":                 "nothing here",
		"pw=hunter2":                   "pw=" + Redacted,
		"hunter2 and hunter2":          Redacted + " and " + Redacted,
		"hunter2hunter2":               Redacted + Redacted,
		"a tok-42 b hunter2 c":         "a " + Redacted + " b " + Redacted + " c",
		"Authorization: Bearer tok-42": "Authorization: Bearer " + Redacted,
		"hunter":                       "hunter",
	}
	for in, want := range cases {
		if got := r.Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
	}
	// Self-overlapping occurrences of one secret are fully covered.
	if got := NewRedactor("aa").Redact("xaaay"); got != "x"+Redacted+"y" {
		t.Errorf("self-overlap: got %q", got)
	}
	// A secret equal to (part of) the marker is still handled without looping.
	if got := NewRedactor("redacted").Redact("redacted"); got != Redacted {
		t.Errorf("marker-like secret: got %q", got)
	}
}

func TestRedactorOverlapping(t *testing.T) {
	if got := NewRedactor("abc", "abcdef").Redact("x abcdef y"); got != "x [redacted] y" {
		t.Fatalf("contained secret: got %q", got)
	}
	// Order of registration must not matter.
	if got := NewRedactor("abcdef", "abc").Redact("x abcdef y abc"); got != "x [redacted] y [redacted]" {
		t.Fatalf("contained secret, reversed: got %q", got)
	}
	// Partial overlaps (neither contains the other) leave no fragment of either.
	r := NewRedactor("abcdef", "efgh")
	for _, in := range []string{"abcdefgh", "efghabcdef", "xabcdefghx"} {
		out := r.Redact(in)
		for _, frag := range []string{"abc", "def", "efg", "gh"} {
			if strings.Contains(out, frag) {
				t.Errorf("Redact(%q) = %q leaks %q", in, out, frag)
			}
		}
	}
	if got := r.Redact("abcdefgh"); got != Redacted {
		t.Errorf("merged span: got %q", got)
	}
	// A shorter secret that starts earlier must not shadow a longer one.
	if got := NewRedactor("xab", "abcdef").Redact("-xabcdef-"); got != "-"+Redacted+"-" {
		t.Errorf("earlier short secret: got %q", got)
	}
}

func TestRedactorNilAndEmpty(t *testing.T) {
	var n *Redactor
	n.Add("SECRET123") // must not panic
	if got := n.Redact("SECRET123 plain"); got != "SECRET123 plain" {
		t.Fatalf("nil Redactor changed text: %q", got)
	}
	r := NewRedactor("", "")
	r.Add("")
	if got := r.Redact("abc"); got != "abc" {
		t.Fatalf("empty secret registered: %q", got)
	}
	var zero Redactor
	if got := zero.Redact("abc"); got != "abc" {
		t.Fatalf("zero Redactor changed text: %q", got)
	}
	zero.Add("b")
	if got := zero.Redact("abc"); got != "a"+Redacted+"c" {
		t.Fatalf("zero Redactor after Add: %q", got)
	}
	// Duplicates are stored once.
	d := NewRedactor("dup", "dup")
	d.Add("dup")
	if c := d.count(); c != 1 {
		t.Fatalf("duplicates stored %d times", c)
	}
	if got := d.Redact("dup"); got != Redacted {
		t.Fatalf("dup: %q", got)
	}
}

func TestRedactorConcurrent(t *testing.T) {
	r := NewRedactor()
	const workers = 16
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			secret := fmt.Sprintf("secret-%02d-value", i)
			r.Add(secret)
			for j := 0; j < 200; j++ {
				// After its own Add, a goroutine's secret must always be masked.
				out := r.Redact("x " + secret + " y")
				if out != "x "+Redacted+" y" {
					t.Errorf("worker %d: got %q", i, out)
					return
				}
				_ = r.Redact(fmt.Sprint(r)) // exercise Format concurrently
			}
		}(i)
	}
	wg.Wait()
	all := make([]string, workers)
	for i := range all {
		all[i] = fmt.Sprintf("secret-%02d-value", i)
	}
	out := r.Redact(strings.Join(all, ","))
	if strings.Contains(out, "secret-") {
		t.Fatalf("secret survived: %q", out)
	}
	if c := r.count(); c != workers {
		t.Fatalf("count = %d, want %d", c, workers)
	}
}

func TestRedactorJSONEscaped(t *testing.T) {
	secret := `pa"ss\word<&>`
	r := NewRedactor(secret)
	body, err := json.Marshal(map[string]string{"key": secret})
	if err != nil {
		t.Fatal(err)
	}
	out := r.Redact(string(body))
	if out != `{"key":"`+Redacted+`"}` {
		t.Fatalf("escaped secret not masked: %q", out)
	}
	// The non-HTML-escaped encoding is masked as well.
	if got := r.Redact(`{"key":"pa\"ss\\word<&>"}`); got != `{"key":"`+Redacted+`"}` {
		t.Fatalf("unescaped-HTML form not masked: %q", got)
	}
	// The raw form is still masked.
	if got := r.Redact("raw " + secret); got != "raw "+Redacted {
		t.Fatalf("raw form not masked: %q", got)
	}
}

func TestSecretRedactor(t *testing.T) {
	t.Setenv("YIPYIP_TEST_KEY", "SECRET123")
	r := NewRedactor(os.Getenv("YIPYIP_TEST_KEY"))
	inputs := []string{
		"SECRET123",
		"SECRET123SECRET123",
		`a"SECRET123"b`,
		"Authorization: Bearer SECRET123",
		`{"error":"bad key SECRET123"}`,
		"line1\nSECRET123\nline3",
	}
	for _, in := range inputs {
		out := r.Redact(in)
		if strings.Contains(out, "SECRET123") || !strings.Contains(out, Redacted) {
			t.Errorf("Redact(%q) = %q", in, out)
		}
	}
	// Formatting the Redactor itself never reveals the secret.
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d"} {
		if s := fmt.Sprintf(verb, r); strings.Contains(s, "SECRET123") || strings.Contains(s, "534543524554313233") {
			t.Errorf("fmt %s revealed secret: %q", verb, s)
		}
	}
	if s := fmt.Sprintf("%+v", struct{ R *Redactor }{r}); strings.Contains(s, "SECRET123") {
		t.Errorf("nested formatting revealed secret: %q", s)
	}
	if b, _ := json.Marshal(r); strings.Contains(string(b), "SECRET123") {
		t.Errorf("json revealed secret: %s", b)
	}
}

func TestSecretStreamRedactor(t *testing.T) {
	r := NewRedactor("SECRET123", "abab")
	texts := []string{
		"key SECRET123 end",
		"SECRET123SECRET123",
		"SECRETX SECRET12 SECRET123",
		"xxababab yy aba",
		`json "SECRET123" tail SECRE`,
		"no secret at all",
		"héllo SECRET123 wörld",
	}
	for _, text := range texts {
		want := r.Redact(text)
		for size := 1; size <= len(text); size++ {
			sr := r.Stream()
			var got strings.Builder
			for i := 0; i < len(text); i += size {
				piece := sr.Write(text[i:min(i+size, len(text))])
				if strings.Contains(piece, "SECRET123") {
					t.Fatalf("piece %q of %q (size %d) leaks", piece, text, size)
				}
				got.WriteString(piece)
			}
			got.WriteString(sr.Flush())
			if got.String() != want {
				t.Fatalf("stream of %q (size %d) = %q, want %q", text, size, got.String(), want)
			}
		}
	}
	// A nil Redactor's stream passes text through at once.
	sr := (*Redactor)(nil).Stream()
	if got := sr.Write("SECRET12") + sr.Write("3"); got != "SECRET123" || sr.Flush() != "" {
		t.Errorf("nil stream = %q", got)
	}
}
