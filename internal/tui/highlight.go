package tui

import (
	"bytes"
	"strings"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// Highlight returns code with ANSI syntax highlighting for lang (chroma
// formatter terminal256, style monokai). An unknown or empty language, or any
// highlighting error, returns code unchanged.
func Highlight(code, lang string) string {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return code
	}
	lexer := lexers.Get(lang)
	if lexer == nil {
		return code
	}
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		return code
	}
	style := styles.Get("monokai")
	if style == nil {
		return code
	}
	it, err := lexer.Tokenise(nil, code)
	if err != nil {
		return code
	}
	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, it); err != nil {
		return code
	}
	return buf.String()
}

// highlightFences highlights every closed fenced code block of text (already
// redacted). The fence lines are dropped from closed blocks; an unclosed
// fence (still streaming) and everything outside fences stay unchanged.
func highlightFences(text string) string {
	if !strings.Contains(text, "```") {
		return text
	}
	lines := strings.Split(text, "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		lang, end, ok := scanFence(lines, i)
		if !ok {
			out = append(out, lines[i])
			continue
		}
		if end < 0 { // unclosed: plain text
			out = append(out, lines[i:]...)
			break
		}
		out = append(out, highlightFenceBody(lines[i+1:end], lang)...)
		i = end
	}
	return strings.Join(out, "\n")
}

// scanFence reports whether lines[i] opens a fenced code block (its trimmed
// text starts with three backticks). lang is the first word after the
// backticks; end is the index of the closing line (trimmed text exactly three
// backticks) or -1 when the fence is still unclosed.
func scanFence(lines []string, i int) (lang string, end int, ok bool) {
	trimmed := strings.TrimSpace(lines[i])
	if !strings.HasPrefix(trimmed, "```") {
		return "", -1, false
	}
	lang = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
	if f := strings.Fields(lang); len(f) > 0 {
		lang = f[0]
	}
	for j := i + 1; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "```" {
			return lang, j, true
		}
	}
	return lang, -1, true
}

// highlightFenceBody highlights the code lines of one closed fence for lang
// and returns the result split into lines.
func highlightFenceBody(code []string, lang string) []string {
	return strings.Split(Highlight(strings.Join(code, "\n"), lang), "\n")
}
