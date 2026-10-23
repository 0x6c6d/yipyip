package tools

import (
	"html"
	"strings"
)

// blockTags are the elements whose opening or closing tag becomes a newline.
var blockTags = map[string]bool{
	"p": true, "div": true, "br": true, "hr": true, "li": true, "ul": true, "ol": true,
	"dl": true, "dt": true, "dd": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"h5": true, "h6": true, "tr": true, "table": true, "thead": true, "tbody": true,
	"section": true, "article": true, "header": true, "footer": true, "nav": true,
	"aside": true, "main": true, "blockquote": true, "pre": true, "form": true,
	"title": true, "head": true, "body": true, "html": true,
}

// rawTags are the elements whose content is dropped up to the matching closing tag.
var rawTags = map[string]bool{"script": true, "style": true, "noscript": true, "template": true}

// maxKnownTag is the length of the longest name in blockTags and rawTags.
const maxKnownTag = len("blockquote")

// htmlToText converts an HTML document to plain text.
func htmlToText(s string) string {
	var b strings.Builder
	b.Grow(len(s) / 2)
	n := len(s)
	i := 0
	for i < n {
		p := strings.IndexByte(s[i:], '<')
		if p < 0 {
			b.WriteString(s[i:])
			break
		}
		b.WriteString(s[i : i+p])
		i += p
		if i+1 >= n {
			b.WriteByte('<')
			break
		}
		switch d := s[i+1]; {
		case d == '!' && strings.HasPrefix(s[i:], "<!--"):
			// Searching from "<!" also accepts the empty comments "<!-->" and "<!--->".
			e := strings.Index(s[i+2:], "-->")
			if e < 0 {
				i = n
			} else {
				i += 2 + e + 3
			}
		case d == '!' || d == '?':
			i = skipPast(s, i+2, '>')
		case d == '/' || isASCIILetter(d):
			i = scanTag(s, i, &b)
		default:
			b.WriteByte('<')
			i++
		}
	}
	text := html.UnescapeString(b.String())
	return normalizeText(strings.ToValidUTF8(text, "�"))
}

// scanTag consumes the tag starting at s[i] == '<' and returns the index after it.
// A block tag writes a newline; the content of a raw-text element is skipped.
func scanTag(s string, i int, b *strings.Builder) int {
	n := len(s)
	j := i + 1
	closing := false
	if s[j] == '/' {
		closing = true
		j++
	}
	start := j
	for j < n && !isTagNameEnd(s[j]) {
		j++
	}
	name := ""
	if j-start <= maxKnownTag {
		name = strings.ToLower(s[start:j])
	}
	end := tagEnd(s, j)
	if blockTags[name] {
		b.WriteByte('\n')
	}
	if closing || !rawTags[name] || end >= n {
		return end
	}
	c := findClosing(s, end, name)
	if c < 0 {
		return n
	}
	return skipPast(s, c+2+len(name), '>')
}

// tagEnd returns the index after the '>' that ends the tag whose attributes start at s[j].
// Quoted attribute values may contain '>'; an unterminated tag or value ends the input.
func tagEnd(s string, j int) int {
	n := len(s)
	afterEq := false
	for j < n {
		c := s[j]
		switch {
		case c == '>':
			return j + 1
		case (c == '"' || c == '\'') && afterEq:
			q := strings.IndexByte(s[j+1:], c)
			if q < 0 {
				return n
			}
			j += 1 + q + 1
			afterEq = false
			continue
		case c == '=':
			afterEq = true
		case isHTMLSpace(c):
		default:
			afterEq = false
		}
		j++
	}
	return n
}

// findClosing returns the index of the "</name" (case-insensitive) that closes a raw-text
// element, searching from s[from], or -1 when there is none.
func findClosing(s string, from int, name string) int {
	n := len(s)
	k := from
	for k < n {
		p := strings.IndexByte(s[k:], '<')
		if p < 0 {
			return -1
		}
		p += k
		q := p + 2 + len(name)
		if q <= n && s[p+1] == '/' && strings.EqualFold(s[p+2:q], name) &&
			(q == n || s[q] == '>' || s[q] == '/' || isHTMLSpace(s[q])) {
			return p
		}
		k = p + 1
	}
	return -1
}

// skipPast returns the index after the first c at or after s[from], or len(s) when there is none.
func skipPast(s string, from int, c byte) int {
	if from >= len(s) {
		return len(s)
	}
	e := strings.IndexByte(s[from:], c)
	if e < 0 {
		return len(s)
	}
	return from + e + 1
}

// normalizeText collapses whitespace within lines, drops empty lines and trims the result.
func normalizeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	var b strings.Builder
	b.Grow(len(s))
	for line := range strings.SplitSeq(s, "\n") {
		first := true
		for f := range strings.FieldsSeq(line) {
			if first {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				first = false
			} else {
				b.WriteByte(' ')
			}
			b.WriteString(f)
		}
	}
	return b.String()
}

func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isHTMLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func isTagNameEnd(c byte) bool {
	return c == '>' || c == '/' || isHTMLSpace(c)
}
