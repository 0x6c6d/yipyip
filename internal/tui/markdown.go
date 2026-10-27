package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// Markdown styles. Tabs are never converted so unstyled bytes stay intact.
var (
	mdBold = lipgloss.NewStyle().Bold(true).TabWidth(lipgloss.NoTabConversion)
	mdH1   = lipgloss.NewStyle().Bold(true).Underline(true).TabWidth(lipgloss.NoTabConversion)
	mdH    = lipgloss.NewStyle().Bold(true).TabWidth(lipgloss.NoTabConversion)
)

// mdBlockRule tries to render the block starting at lines[i]. It returns the
// rendered lines, the index of the first line it did not consume and whether
// it applied at all. width is the available column count (<= 0: unknown).
type mdBlockRule func(lines []string, i, width int) (out []string, next int, ok bool)

// mdBlockRules are tried in order for every block; the last one always applies.
var mdBlockRules = []mdBlockRule{
	mdFenceRule,
	mdHeadingRule,
	mdParagraphRule,
}

// renderMarkdown renders assistant markdown (already redacted) as styled terminal text.
func renderMarkdown(src string, width int) string {
	lines := strings.Split(src, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		for _, rule := range mdBlockRules {
			rendered, next, ok := rule(lines, i, width)
			if !ok {
				continue
			}
			out = append(out, rendered...)
			if next <= i { // a rule must make progress
				next = i + 1
			}
			i = next
			break
		}
	}
	return strings.Join(out, "\n")
}

// mdFenceRule renders a fenced code block: a closed fence is replaced by its
// highlighted code (fence lines dropped); an unclosed fence (still streaming)
// emits the opening line and every remaining line unchanged.
func mdFenceRule(lines []string, i, _ int) ([]string, int, bool) {
	lang, end, ok := scanFence(lines, i)
	if !ok {
		return nil, i, false
	}
	if end < 0 {
		return lines[i:], len(lines), true
	}
	return highlightFenceBody(lines[i+1:end], lang), end + 1, true
}

// mdHeadingRule renders an ATX heading (up to 3 leading spaces, 1 to 6 '#',
// then a space or the end of the line) as bold text; level 1 is also
// underlined. A closing run of '#' preceded by a space is removed.
func mdHeadingRule(lines []string, i, _ int) ([]string, int, bool) {
	level, text, ok := parseATXHeading(lines[i])
	if !ok {
		return nil, i, false
	}
	style := mdH
	if level == 1 {
		style = mdH1
	}
	return []string{renderInlineWith(text, &style)}, i + 1, true
}

// parseATXHeading returns the level and text of an ATX heading line.
func parseATXHeading(line string) (level int, text string, ok bool) {
	p := 0
	for p < len(line) && p < 3 && line[p] == ' ' {
		p++
	}
	for p+level < len(line) && line[p+level] == '#' {
		level++
	}
	if level < 1 || level > 6 {
		return 0, "", false
	}
	rest := line[p+level:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return 0, "", false
	}
	text = strings.Trim(rest, " \t")
	if k := strings.TrimRight(text, "#"); k == "" {
		text = ""
	} else if len(k) < len(text) && (k[len(k)-1] == ' ' || k[len(k)-1] == '\t') {
		text = strings.TrimRight(k, " \t")
	}
	return level, text, true
}

// mdParagraphRule renders any other line through the inline pass.
func mdParagraphRule(lines []string, i, _ int) ([]string, int, bool) {
	return []string{renderInline(lines[i])}, i + 1, true
}

// renderInline renders the inline markdown of one line: code spans are kept
// verbatim, **bold** and __bold__ are styled, everything else is copied.
func renderInline(s string) string {
	return renderInlineWith(s, nil)
}

// renderInlineWith renders the inline markdown of s; plain text is rendered
// with base (copied unchanged when base is nil) and bold spans with base
// made bold.
func renderInlineWith(s string, base *lipgloss.Style) string {
	var out, lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			out.WriteString(mdStyled(lit.String(), base))
			lit.Reset()
		}
	}
	// Once no closer exists after an opener, none exists for later openers.
	noCloser := map[byte]bool{}
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '`':
			n := mdRunLen(s, i, '`')
			if end := mdCodeSpanEnd(s, i, n); end > 0 {
				lit.WriteString(s[i:end])
				i = end
				continue
			}
			lit.WriteString(s[i : i+n])
			i += n
		case (c == '*' || c == '_') && i+1 < len(s) && s[i+1] == c:
			if !noCloser[c] && mdCanOpen(s, i, c) {
				if j := mdFindCloser(s, i+2, c); j >= 0 {
					flush()
					out.WriteString(renderInlineWith(s[i+2:j], mdBoldOf(base)))
					i = j + 2
					continue
				}
				noCloser[c] = true
			}
			lit.WriteString(s[i : i+2])
			i += 2
		default:
			lit.WriteByte(c)
			i++
		}
	}
	flush()
	return out.String()
}

// mdStyled renders text with style, or returns it unchanged for a nil style.
func mdStyled(text string, style *lipgloss.Style) string {
	if style == nil || text == "" {
		return text
	}
	return style.Render(text)
}

// mdBoldOf returns base made bold (mdBold for a nil base).
func mdBoldOf(base *lipgloss.Style) *lipgloss.Style {
	if base == nil {
		return &mdBold
	}
	b := base.Bold(true)
	return &b
}

// mdRunLen returns the length of the run of byte c starting at s[i].
func mdRunLen(s string, i int, c byte) int {
	n := 0
	for i+n < len(s) && s[i+n] == c {
		n++
	}
	return n
}

// mdCodeSpanEnd returns the index just past the run of exactly n backticks
// that closes the code span opened by the n backticks at s[i], or -1.
func mdCodeSpanEnd(s string, i, n int) int {
	for j := i + n; j < len(s); {
		if s[j] != '`' {
			j++
			continue
		}
		m := mdRunLen(s, j, '`')
		if m == n {
			return j + m
		}
		j += m
	}
	return -1
}

// mdCanOpen reports whether the doubled marker c at s[i] can open a bold span.
func mdCanOpen(s string, i int, c byte) bool {
	next, _ := utf8.DecodeRuneInString(s[i+2:])
	if i+2 >= len(s) || unicode.IsSpace(next) {
		return false
	}
	if c == '_' && i > 0 {
		prev, _ := utf8.DecodeLastRuneInString(s[:i])
		if unicode.IsLetter(prev) || unicode.IsDigit(prev) {
			return false
		}
	}
	return true
}

// mdCanClose reports whether the doubled marker c at s[j] can close a bold span.
func mdCanClose(s string, j int, c byte) bool {
	if j == 0 {
		return false
	}
	prev, _ := utf8.DecodeLastRuneInString(s[:j])
	if unicode.IsSpace(prev) {
		return false
	}
	if c == '_' && j+2 < len(s) {
		next, _ := utf8.DecodeRuneInString(s[j+2:])
		if unicode.IsLetter(next) || unicode.IsDigit(next) {
			return false
		}
	}
	return true
}

// mdFindCloser returns the index of the doubled marker c that closes a bold
// span whose content starts at s[from] (content non-empty, code spans
// skipped), or -1.
func mdFindCloser(s string, from int, c byte) int {
	for j := from; j < len(s); {
		switch {
		case s[j] == '`':
			n := mdRunLen(s, j, '`')
			if end := mdCodeSpanEnd(s, j, n); end > 0 {
				j = end
			} else {
				j += n
			}
		case j > from && s[j] == c && j+1 < len(s) && s[j+1] == c && mdCanClose(s, j, c):
			return j
		default:
			j++
		}
	}
	return -1
}
