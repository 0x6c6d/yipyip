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
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "```") {
			out = append(out, lines[i])
			continue
		}
		lang := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
		if f := strings.Fields(lang); len(f) > 0 {
			lang = f[0]
		}
		end := -1
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "```" {
				end = j
				break
			}
		}
		if end < 0 { // unclosed: plain text
			out = append(out, lines[i:]...)
			break
		}
		code := strings.Join(lines[i+1:end], "\n")
		out = append(out, strings.Split(Highlight(code, lang), "\n")...)
		i = end
	}
	return strings.Join(out, "\n")
}
