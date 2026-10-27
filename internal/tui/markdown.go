package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Markdown styles. Tabs are never converted so unstyled bytes stay intact.
var (
	mdBold = lipgloss.NewStyle().Bold(true).TabWidth(lipgloss.NoTabConversion)
	mdH1   = lipgloss.NewStyle().Bold(true).Underline(true).TabWidth(lipgloss.NoTabConversion)
	mdH    = lipgloss.NewStyle().Bold(true).TabWidth(lipgloss.NoTabConversion)
	mdDim  = lipgloss.NewStyle().Faint(true)
)

// mdBlockRule tries to render the block starting at lines[i]. It returns the
// rendered lines, the index of the first line it did not consume and whether
// it applied at all. width is the available column count (<= 0: unknown).
type mdBlockRule func(lines []string, i, width int) (out []string, next int, ok bool)

// mdMaxQuoteDepth is the deepest quote nesting rendered with block rules;
// the text inside the innermost quote becomes plain paragraph lines.
const mdMaxQuoteDepth = 32

// mdRuleWidth is the width of a horizontal rule when the width is unknown.
const mdRuleWidth = 40

// mdBlockRulesAt returns the block rules for text nested in depth quotes,
// tried in order for every block; the last one always applies.
func mdBlockRulesAt(depth int) []mdBlockRule {
	return []mdBlockRule{
		mdFenceRule,
		mdTableRule,
		mdHeadingRule,
		mdHRuleRule,
		mdQuoteRuleAt(depth),
		mdListRule,
		mdParagraphRule,
	}
}

// renderMarkdown renders assistant markdown (already redacted) as styled terminal text.
func renderMarkdown(src string, width int) string {
	return mdRender(src, width, 0)
}

// mdRender renders src as markdown nested in depth quotes.
func mdRender(src string, width, depth int) string {
	lines := strings.Split(src, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	rules := mdBlockRulesAt(depth)
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		for _, rule := range rules {
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

// mdHRuleRule renders a horizontal rule: a line that is, apart from leading
// and trailing whitespace, three or more of one of '-', '*' or '_' and
// nothing else. It spans width columns (mdRuleWidth when width <= 0).
func mdHRuleRule(lines []string, i, width int) ([]string, int, bool) {
	s := strings.TrimSpace(lines[i])
	if len(s) < 3 || (s[0] != '-' && s[0] != '*' && s[0] != '_') || mdRunLen(s, 0, s[0]) != len(s) {
		return nil, i, false
	}
	if width <= 0 {
		width = mdRuleWidth
	}
	return []string{mdDim.Render(strings.Repeat("─", width))}, i + 1, true
}

// mdQuoteRuleAt returns the blockquote rule for text nested in depth quotes:
// the maximal run of lines starting, after up to 3 spaces, with '>'. The '>'
// and one following space are stripped, the inner text is rendered as
// markdown two columns narrower and every line gets a "│ " bar. The quote at
// mdMaxQuoteDepth renders its inner text as plain paragraph lines, so the
// recursion stays bounded.
func mdQuoteRuleAt(depth int) mdBlockRule {
	return func(lines []string, i, width int) ([]string, int, bool) {
		var inner []string
		next := i
		for ; next < len(lines); next++ {
			text, ok := mdQuoteLine(lines[next])
			if !ok {
				break
			}
			inner = append(inner, text)
		}
		if next == i {
			return nil, i, false
		}
		if width > 0 {
			width = max(width-2, 1)
		}
		var body []string
		if depth+1 >= mdMaxQuoteDepth {
			for _, l := range inner {
				body = append(body, renderInline(l))
			}
		} else {
			body = strings.Split(mdRender(strings.Join(inner, "\n"), width, depth+1), "\n")
		}
		bar := mdDim.Render("│") + " "
		for k, l := range body {
			body[k] = bar + l
		}
		return body, next, true
	}
}

// mdQuoteLine reports whether line is a quote line (up to 3 spaces, then
// '>') and returns its text without the '>' and one following space.
func mdQuoteLine(line string) (string, bool) {
	p := mdRunLen(line, 0, ' ')
	if p > 3 || p >= len(line) || line[p] != '>' {
		return "", false
	}
	return strings.TrimPrefix(line[p+1:], " "), true
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

// mdAlign is the alignment of a table column.
type mdAlign int

// Table column alignments.
const (
	mdAlignLeft mdAlign = iota
	mdAlignRight
	mdAlignCenter
)

// mdTableRule renders a table: a header row with at least one cell separator,
// directly followed by a separator row with the same cell count, then every
// following row that has a cell separator. Without a valid separator row the
// lines are left to the other rules (a half-streamed table stays plain text).
// A table wider than width (> 0) gets narrower columns with wrapped cells; a
// table that cannot fit even then is emitted as its unchanged source lines.
func mdTableRule(lines []string, i, width int) ([]string, int, bool) {
	if i+1 >= len(lines) {
		return nil, i, false
	}
	header, seps := mdSplitRow(lines[i])
	if seps == 0 {
		return nil, i, false
	}
	aligns, ok := mdParseSeparator(lines[i+1], len(header))
	if !ok {
		return nil, i, false
	}
	n := len(header)
	cells := [][]string{make([]string, n)}
	for c, text := range header {
		cells[0][c] = renderInlineWith(text, &mdBold)
	}
	next := i + 2
	for ; next < len(lines); next++ {
		row, seps := mdSplitRow(lines[next])
		if seps == 0 {
			break
		}
		r := make([]string, n) // missing cells stay empty, extra cells are dropped
		for c := 0; c < n && c < len(row); c++ {
			r[c] = renderInline(row[c])
		}
		cells = append(cells, r)
	}
	widths := make([]int, n)
	for _, r := range cells {
		for c, cell := range r {
			widths[c] = max(widths[c], ansi.StringWidth(cell))
		}
	}
	if width <= 0 || mdTableWidth(widths) <= width {
		return mdTableLayout(cells, 1, aligns, widths), next, true
	}
	if !mdShrinkColumns(widths, width) {
		return lines[i:next], next, true
	}
	wrapped, headerRows, ok := mdWrapCells(cells, widths)
	if !ok {
		return lines[i:next], next, true
	}
	return mdTableLayout(wrapped, headerRows, aligns, widths), next, true
}

// mdTableWidth is the width of every rendered table line for the given
// column content widths: the cells, one space of padding on each side and
// a border before, between and after the columns.
func mdTableWidth(widths []int) int {
	total := 3*len(widths) + 1
	for _, w := range widths {
		total += w
	}
	return total
}

// mdShrinkColumns narrows the natural column widths in place until the table
// fits width: the currently widest column (the leftmost on a tie) loses one
// column at a time, never going below min(3, its natural width). It reports
// false, leaving widths unchanged, when even the minimum widths do not fit.
func mdShrinkColumns(widths []int, width int) bool {
	mins := make([]int, len(widths))
	for c, w := range widths {
		mins[c] = min(3, w)
	}
	if mdTableWidth(mins) > width {
		return false
	}
	// One column at a time is equivalent to lowering the widest level as a
	// whole: every column at the top level M (all above their minimum, as
	// min <= 3 < M) drops to the next level below, and a partial level ends
	// with its leftmost columns one lower. This keeps long cells cheap.
	excess := mdTableWidth(widths) - width
	for excess > 0 {
		top, next := 0, 3
		for _, w := range widths {
			top = max(top, w)
		}
		var at []int
		for c, w := range widths {
			if w == top {
				at = append(at, c)
			} else {
				next = max(next, w)
			}
		}
		k := len(at)
		if full := k * (top - next); excess >= full {
			for _, c := range at {
				widths[c] = next
			}
			excess -= full
			continue
		}
		for j, c := range at {
			widths[c] = top - excess/k
			if j < excess%k {
				widths[c]--
			}
		}
		excess = 0
	}
	return true
}

// mdWrapCells wraps every rendered cell to its column width and splits each
// row into physical rows: a row becomes as many rows as its tallest cell has
// lines, shorter cells get empty lines. Styles open at a line break are closed
// at the end of the line and reopened on the next one, so each physical line
// is styled on its own. ok is false if a line is still wider than its column
// (a grapheme wider than the column), so the caller never overflows. header
// is the number of physical rows of the header row.
func mdWrapCells(cells [][]string, widths []int) (out [][]string, header int, ok bool) {
	for k, r := range cells {
		parts := make([][]string, len(r))
		height := 1
		for c, cell := range r {
			parts[c] = strings.Split(mdCarryStyles(ansi.Wrap(cell, widths[c], "")), "\n")
			for _, l := range parts[c] {
				if ansi.StringWidth(l) > widths[c] {
					return nil, 0, false
				}
			}
			height = max(height, len(parts[c]))
		}
		for line := 0; line < height; line++ {
			phys := make([]string, len(r))
			for c := range r {
				if line < len(parts[c]) {
					phys[c] = parts[c][line]
				}
			}
			out = append(out, phys)
		}
		if k == 0 {
			header = height
		}
	}
	return out, header, true
}

// mdCarryStyles makes every line of s carry its own SGR styling: at each
// newline the SGR sequences still in effect (since the last reset) are
// closed with a reset and repeated at the start of the next line.
func mdCarryStyles(s string) string {
	if !strings.Contains(s, "\x1b[") || !strings.Contains(s, "\n") {
		return s
	}
	var out, active strings.Builder
	for i := 0; i < len(s); {
		switch {
		case s[i] == '\n':
			if active.Len() > 0 {
				out.WriteString("\x1b[0m\n")
				out.WriteString(active.String())
			} else {
				out.WriteByte('\n')
			}
			i++
		case s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[':
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j == len(s) {
				out.WriteString(s[i:])
				return out.String()
			}
			seq := s[i : j+1]
			out.WriteString(seq)
			if s[j] == 'm' {
				if params := s[i+2 : j]; params == "" || params == "0" || strings.HasPrefix(params, "0;") {
					active.Reset()
				}
				if params := s[i+2 : j]; params != "" && params != "0" {
					active.WriteString(seq)
				}
			}
			i = j + 1
		default:
			out.WriteByte(s[i])
			i++
		}
	}
	return out.String()
}

// mdParseSeparator parses a table separator row with n cells and returns the
// column alignments; ok is false when line is not such a row.
func mdParseSeparator(line string, n int) ([]mdAlign, bool) {
	if !strings.Contains(line, "|") || strings.Trim(line, "|-: ") != "" {
		return nil, false
	}
	cells, _ := mdSplitRow(line)
	if len(cells) != n {
		return nil, false
	}
	aligns := make([]mdAlign, n)
	for c, cell := range cells {
		left, right := strings.HasPrefix(cell, ":"), strings.HasSuffix(cell, ":")
		dashes := strings.TrimSuffix(strings.TrimPrefix(cell, ":"), ":")
		if dashes == "" || strings.Trim(dashes, "-") != "" {
			return nil, false
		}
		switch {
		case left && right:
			aligns[c] = mdAlignCenter
		case right:
			aligns[c] = mdAlignRight
		default:
			aligns[c] = mdAlignLeft
		}
	}
	return aligns, true
}

// mdSplitRow splits a table row into trimmed cells and returns them with the
// number of cell separators found. One leading and one trailing '|' are
// dropped; a '|' preceded by a backslash or inside a code span does not
// split, and "\|" becomes "|" in the cell text.
func mdSplitRow(line string) (cells []string, seps int) {
	s := strings.TrimSpace(line)
	var code []int
	var cell strings.Builder
	closed := false
	split := func() {
		cells = append(cells, strings.TrimSpace(strings.ReplaceAll(cell.String(), `\|`, "|")))
		cell.Reset()
	}
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c == '`':
			if code == nil {
				code = mdCodeEnds(s)
			}
			end := code[i]
			if end < 0 {
				end = i + mdRunLen(s, i, '`')
			}
			cell.WriteString(s[i:end])
			i = end
		case c == '\\' && i+1 < len(s) && s[i+1] == '|':
			cell.WriteString(s[i : i+2])
			i += 2
		case c == '|':
			seps++
			if i > 0 { // a leading '|' opens the first cell
				split()
				closed = i == len(s)-1
			}
			i++
		default:
			cell.WriteByte(c)
			i++
		}
	}
	if !closed {
		split() // the last cell, unless a trailing '|' already closed it
	}
	return cells, seps
}

// mdTableLayout draws a table of rendered cells (the first header rows are
// the header) with box-drawing borders; widths are the column widths and
// every cell is padded to its column's width with that column's alignment.
func mdTableLayout(cells [][]string, header int, aligns []mdAlign, widths []int) []string {
	rule := func(left, mid, right string) string {
		parts := make([]string, len(widths))
		for c, w := range widths {
			parts[c] = strings.Repeat("─", w+2)
		}
		return left + strings.Join(parts, mid) + right
	}
	row := func(r []string) string {
		parts := make([]string, len(widths))
		for c, w := range widths {
			parts[c] = " " + mdPad(r[c], w, aligns[c]) + " "
		}
		return "│" + strings.Join(parts, "│") + "│"
	}
	out := make([]string, 0, len(cells)+3)
	out = append(out, rule("┌", "┬", "┐"))
	for k, r := range cells {
		out = append(out, row(r))
		if k == header-1 {
			out = append(out, rule("├", "┼", "┤"))
		}
	}
	return append(out, rule("└", "┴", "┘"))
}

// mdPad pads the rendered cell to width w with the given alignment.
func mdPad(cell string, w int, align mdAlign) string {
	gap := max(w-ansi.StringWidth(cell), 0)
	switch align {
	case mdAlignRight:
		return strings.Repeat(" ", gap) + cell
	case mdAlignCenter:
		return strings.Repeat(" ", gap/2) + cell + strings.Repeat(" ", gap-gap/2)
	default:
		return cell + strings.Repeat(" ", gap)
	}
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

// mdListItem is one parsed bullet or numbered list item line.
type mdListItem struct {
	indent  int    // leading columns (a tab counts as 4)
	marker  string // rendered marker: "•", or the number and its '.' or ')' as written
	text    string // the text after the marker and its spaces
	textCol int    // source column of the text: indent + marker + spaces
}

// mdListRule renders a bullet or numbered list starting at an item line: the
// maximal run of item lines, continuation lines (indented at least to the
// text column of the item above) and blank lines followed by another item or
// continuation line. A non-item line that opens a code fence ends the list,
// so the fence rule still highlights code inside list items. Items are nested
// by indent: the first item with an indent > 0 sets the unit (2 below 4
// columns, else 4) and each level is rendered two columns deeper.
func mdListRule(lines []string, i, _ int) ([]string, int, bool) {
	first, ok := mdParseListItem(lines[i])
	if !ok {
		return nil, i, false
	}
	end, col := i+1, first.textCol
	for j := end; j < len(lines); {
		k := j
		for k < len(lines) && strings.TrimSpace(lines[k]) == "" {
			k++
		}
		if k == len(lines) {
			break // trailing blank lines belong to what follows
		}
		if it, ok := mdParseListItem(lines[k]); ok {
			col = it.textCol
		} else if indent, _ := mdIndent(lines[k]); indent < col {
			break
		} else if _, _, fence := scanFence(lines, k); fence {
			break
		}
		end, j = k+1, k+1
	}
	unit := 0
	for _, l := range lines[i:end] {
		if it, ok := mdParseListItem(l); ok && it.indent > 0 {
			unit = 2
			if it.indent >= 4 {
				unit = 4
			}
			break
		}
	}
	out := make([]string, 0, end-i)
	textCol := 0 // rendered text column of the current item
	for _, l := range lines[i:end] {
		if strings.TrimSpace(l) == "" {
			out = append(out, "")
			continue
		}
		it, ok := mdParseListItem(l)
		if !ok {
			out = append(out, strings.Repeat(" ", textCol)+renderInline(strings.TrimSpace(l)))
			continue
		}
		level := 0
		if unit > 0 {
			level = it.indent / unit
		}
		prefix := strings.Repeat(" ", 2*level) + it.marker + " "
		textCol = ansi.StringWidth(prefix)
		out = append(out, prefix+renderInline(it.text))
	}
	return out, end, true
}

// mdParseListItem parses a list item line: an indent, then a bullet marker
// ('-', '*' or '+') or 1 to 9 digits followed by '.' or ')', then at least
// one space and the item text.
func mdParseListItem(line string) (mdListItem, bool) {
	indent, p := mdIndent(line)
	rest := line[p:]
	if rest == "" {
		return mdListItem{}, false
	}
	n := 0
	marker := "•"
	if c := rest[0]; c == '-' || c == '*' || c == '+' {
		n = 1
	} else {
		for n < len(rest) && n <= 9 && rest[n] >= '0' && rest[n] <= '9' {
			n++
		}
		if n == 0 || n > 9 || n >= len(rest) || (rest[n] != '.' && rest[n] != ')') {
			return mdListItem{}, false
		}
		n++
		marker = rest[:n]
	}
	spaces := mdRunLen(rest, n, ' ')
	if spaces == 0 {
		return mdListItem{}, false
	}
	return mdListItem{indent: indent, marker: marker, text: rest[n+spaces:], textCol: indent + n + spaces}, true
}

// mdIndent returns the indent of line in columns (a space counts as 1, a tab
// as 4) and the number of leading whitespace bytes.
func mdIndent(line string) (cols, n int) {
	for ; n < len(line); n++ {
		switch line[n] {
		case ' ':
			cols++
		case '\t':
			cols += 4
		default:
			return cols, n
		}
	}
	return cols, n
}

// mdParagraphRule renders any other line through the inline pass.
func mdParagraphRule(lines []string, i, _ int) ([]string, int, bool) {
	return []string{renderInline(lines[i])}, i + 1, true
}

// renderInline renders the inline markdown of one line: backslash escapes,
// code spans, links, autolinks and bold, italic and strikethrough emphasis
// are rendered without their markup; everything else is copied.
func renderInline(s string) string {
	return renderInlineWith(s, nil)
}

// mdEscapable lists the characters a backslash escapes.
const mdEscapable = "\\`*_{}[]()#+-.!|~<>"

// mdCodeColor is the foreground colour of inline code spans.
var mdCodeColor = lipgloss.Color("#B1B9F9")

// mdCode is the style of an inline code span outside any other style.
var mdCode = lipgloss.NewStyle().Foreground(mdCodeColor).TabWidth(lipgloss.NoTabConversion)

// mdPlain is the base style that emphasis adds to outside any other style.
var mdPlain = lipgloss.NewStyle().TabWidth(lipgloss.NoTabConversion)

// mdMaxInlineDepth is the deepest nesting of emphasis and link text that is
// rendered; text nested deeper is copied with the style around it, so the
// recursion (and the rescanning of nested text) stays bounded.
const mdMaxInlineDepth = 16

// renderInlineWith renders the inline markdown of s; plain text is rendered
// with base (copied unchanged when base is nil) and every styled form with
// base plus its own style. The text is scanned left to right and at each
// position the first matching form wins: backslash escape, code span, image
// (kept literally), link, autolink, emphasis.
func renderInlineWith(s string, base *lipgloss.Style) string {
	return mdInline(s, base, 0)
}

// mdInline is renderInlineWith for text nested in depth emphasis or link
// spans. Every lookup ahead (code span ends, link brackets, emphasis
// closers) uses a linear index or a memo, so each nesting level scans s a
// bounded number of times.
func mdInline(s string, base *lipgloss.Style, depth int) string {
	if depth >= mdMaxInlineDepth {
		return mdStyled(s, base)
	}
	var out, lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			out.WriteString(mdStyled(lit.String(), base))
			lit.Reset()
		}
	}
	var ix *mdInlineIndex // built on the first lookup that needs it
	index := func() *mdInlineIndex {
		if ix == nil {
			ix = newMdInlineIndex(s)
		}
		return ix
	}
	// Once no closer exists after an opener, none exists for later openers
	// of the same marker (the closer search range only shrinks).
	noCloser := map[string]bool{}
	for i := 0; i < len(s); {
		c := s[i]
		switch c {
		case '\\':
			if i+1 < len(s) && strings.IndexByte(mdEscapable, s[i+1]) >= 0 {
				lit.WriteByte(s[i+1])
				i += 2
				continue
			}
			lit.WriteByte(c)
			i++
		case '`':
			n := mdRunLen(s, i, '`')
			if end := index().code[i]; end > 0 {
				flush()
				out.WriteString(mdStyled(mdCodeContent(s[i+n:end-n]), mdCodeOf(base)))
				i = end
				continue
			}
			lit.WriteString(s[i : i+n])
			i += n
		case '!':
			if i+1 < len(s) && s[i+1] == '[' {
				if end, _, _, ok := index().link(i + 1); ok {
					lit.WriteString(s[i:end]) // images are not supported: all characters stay
					i = end
					continue
				}
			}
			lit.WriteByte(c)
			i++
		case '[':
			if end, text, url, ok := index().link(i); ok {
				flush()
				out.WriteString(mdRenderLink(text, url, base, depth+1))
				i = end
				continue
			}
			lit.WriteByte(c)
			i++
		case '<':
			if end, url, ok := mdParseAutolink(s, i); ok {
				lit.WriteString(url)
				i = end
				continue
			}
			lit.WriteByte(c)
			i++
		case '*', '_', '~':
			n := 1
			if i+1 < len(s) && s[i+1] == c {
				n = 2
			}
			marker := s[i : i+n]
			if c == '~' && n == 1 {
				lit.WriteByte(c)
				i++
				continue
			}
			if !noCloser[marker] && mdCanOpen(s, i, c, n) {
				if j := mdFindCloser(s, index().code, i+n, c, n); j >= 0 {
					flush()
					out.WriteString(mdInline(s[i+n:j], mdEmphasisOf(base, c, n), depth+1))
					i = j + n
					continue
				}
				noCloser[marker] = true
			}
			lit.WriteString(marker)
			i += n
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

// mdEmphasisOf returns base with the style of the emphasis marker c repeated
// n times: "**"/"__" bold, "*"/"_" italic, "~~" strikethrough.
func mdEmphasisOf(base *lipgloss.Style, c byte, n int) *lipgloss.Style {
	if c != '~' && n == 2 {
		return mdBoldOf(base)
	}
	s := mdPlain
	if base != nil {
		s = *base
	}
	if c == '~' {
		s = s.Strikethrough(true)
	} else {
		s = s.Italic(true)
	}
	return &s
}

// mdCodeOf returns base with the code span colour (mdCode for a nil base).
func mdCodeOf(base *lipgloss.Style) *lipgloss.Style {
	if base == nil {
		return &mdCode
	}
	s := base.Foreground(mdCodeColor)
	return &s
}

// mdCodeContent returns the text of a code span: one leading and one
// trailing space are stripped when both exist and it is not all spaces.
func mdCodeContent(content string) string {
	if len(content) >= 2 && content[0] == ' ' && content[len(content)-1] == ' ' && strings.Trim(content, " ") != "" {
		return content[1 : len(content)-1]
	}
	return content
}

// mdInlineIndex holds the lookups ahead of one string of the inline pass,
// each built in one linear pass, so no lookup rescans the rest of the string.
type mdInlineIndex struct {
	s     string
	code  []int // per backtick: mdCodeEnds
	close []int // per '[': the index of its matching ']' or -1
	stop  []int // per index (and len(s)): the first index >= it with ')' or a space, or len(s)
}

// newMdInlineIndex indexes s for the inline pass.
//
// A link scan from a '[' walks tokens left to right (an escape is its
// backslash and the next byte, a code span or an unclosed backtick run is
// one token, any other byte is one token), counts '[' up and ']' down and
// stops when the count returns to zero. Every position has exactly one
// next token, so the walks of all '[' form a tree towards len(s): up[p] is
// the first node after p on its walk whose count is one higher (relative
// to p's walk), and the ']' matching a '[' at j sits just before
// up[j+1]. Nodes are filled from the end, as every node's successor lies
// after it.
func newMdInlineIndex(s string) *mdInlineIndex {
	n := len(s)
	ix := &mdInlineIndex{s: s, code: mdCodeEnds(s), close: make([]int, n), stop: make([]int, n+1)}
	up := make([]int, n+1)
	up[n] = -1
	ix.stop[n] = n
	runEnd := n // the end of the backtick run containing p
	for p := n - 1; p >= 0; p-- {
		if c := s[p]; c == ')' || unicode.IsSpace(rune(c)) {
			ix.stop[p] = p
		} else {
			ix.stop[p] = ix.stop[p+1]
		}
		if s[p] != '`' {
			runEnd = p
		}
		next := p + 1
		switch s[p] {
		case '\\':
			next = min(p+2, n)
		case '`':
			if next = ix.code[p]; next < 0 {
				next = runEnd
			}
		}
		switch s[p] {
		case ']':
			up[p] = next
		case '[':
			if up[next] >= 0 {
				up[p] = up[up[next]]
			} else {
				up[p] = -1
			}
		default:
			up[p] = up[next]
		}
	}
	for p := 0; p < n; p++ {
		ix.close[p] = -1
		if s[p] == '[' && up[p+1] >= 0 {
			ix.close[p] = up[p+1] - 1
		}
	}
	return ix
}

// mdCodeEnds returns, for every index i of s holding a backtick, the index
// just past the run of exactly as many backticks as the run starting at
// s[i] has that closes the code span opened there, or -1 (any other index:
// unspecified). Built in one pass from the end.
func mdCodeEnds(s string) []int {
	ends := make([]int, len(s))
	later := map[int]int{} // run length → start of the nearest run after the current one
	for b := len(s); b > 0; {
		if s[b-1] != '`' {
			b--
			continue
		}
		a := b - 1
		for a > 0 && s[a-1] == '`' {
			a--
		}
		for k := a; k < b; k++ {
			ends[k] = -1
			if start, ok := later[b-k]; ok {
				ends[k] = start + b - k
			}
		}
		later[b-a] = a
		b = a
	}
	return ends
}

// link parses a link "[text](url)" starting at s[i]: '[' up to the matching
// ']' (nested brackets balanced; escapes and code spans skipped), directly
// followed by '(', a non-empty url without whitespace and ')'. It returns
// the index just past the ')' and the text and url.
func (ix *mdInlineIndex) link(i int) (end int, text, url string, ok bool) {
	s := ix.s
	if i >= len(s) || s[i] != '[' {
		return 0, "", "", false
	}
	k := ix.close[i]
	if k < 0 || k+1 >= len(s) || s[k+1] != '(' {
		return 0, "", "", false
	}
	u := ix.stop[k+2]
	if u >= len(s) || s[u] != ')' || u == k+2 {
		return 0, "", "", false
	}
	return u + 1, s[i+1 : k], s[k+2 : u], true
}

// mdRenderLink renders a link as its inline-rendered text (nested depth
// spans deep) followed by the url in parentheses, or as the url alone when
// the text shows the url.
func mdRenderLink(text, url string, base *lipgloss.Style, depth int) string {
	rendered := mdInline(text, base, depth)
	if ansi.Strip(rendered) == url {
		return mdStyled(url, base)
	}
	return rendered + mdStyled(" ("+url+")", base)
}

// mdParseAutolink parses an autolink "<http://…>" or "<https://…>" starting
// at s[i] (no whitespace, '<' or '>' inside the url) and returns the index
// just past the '>' and the url.
func mdParseAutolink(s string, i int) (end int, url string, ok bool) {
	rest := s[i+1:]
	var scheme int
	switch {
	case strings.HasPrefix(rest, "http://"):
		scheme = len("http://")
	case strings.HasPrefix(rest, "https://"):
		scheme = len("https://")
	default:
		return 0, "", false
	}
	for k := scheme; k < len(rest); k++ {
		switch c := rest[k]; {
		case c == '>':
			if k == scheme {
				return 0, "", false
			}
			return i + 1 + k + 1, rest[:k], true
		case c == '<' || unicode.IsSpace(rune(c)):
			return 0, "", false
		}
	}
	return 0, "", false
}

// mdRunLen returns the length of the run of byte c starting at s[i].
func mdRunLen(s string, i int, c byte) int {
	n := 0
	for i+n < len(s) && s[i+n] == c {
		n++
	}
	return n
}

// mdAlnumAt reports whether the rune before s[i] (before: true) or at s[i]
// is a letter or digit.
func mdAlnumAt(s string, i int, before bool) bool {
	var r rune
	if before {
		if i <= 0 {
			return false
		}
		r, _ = utf8.DecodeLastRuneInString(s[:i])
	} else {
		if i >= len(s) {
			return false
		}
		r, _ = utf8.DecodeRuneInString(s[i:])
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// mdCanOpen reports whether the marker c repeated n times at s[i] can open
// an emphasis span: it is followed by a non-space and, for '_', not
// preceded by a letter or digit.
func mdCanOpen(s string, i int, c byte, n int) bool {
	if i+n >= len(s) {
		return false
	}
	if next, _ := utf8.DecodeRuneInString(s[i+n:]); unicode.IsSpace(next) {
		return false
	}
	return c != '_' || !mdAlnumAt(s, i, true)
}

// mdCanClose reports whether the marker c repeated n times at s[j] can close
// an emphasis span: it is preceded by a non-space and, for '_', not followed
// by a letter or digit.
func mdCanClose(s string, j int, c byte, n int) bool {
	if j == 0 {
		return false
	}
	if prev, _ := utf8.DecodeLastRuneInString(s[:j]); unicode.IsSpace(prev) {
		return false
	}
	return c != '_' || !mdAlnumAt(s, j+n, false)
}

// mdIsMarker reports whether s[j:] starts the marker c repeated n times; a
// single marker must not be next to another c, so it never matches half
// of a doubled one.
func mdIsMarker(s string, j int, c byte, n int) bool {
	if n == 2 {
		return j+1 < len(s) && s[j] == c && s[j+1] == c
	}
	return s[j] == c && (j+1 >= len(s) || s[j+1] != c) && (j == 0 || s[j-1] != c)
}

// mdFindCloser returns the index of the nearest marker c repeated n times
// that closes an emphasis span whose content starts at s[from] (content
// non-empty; code spans, ends from mdCodeEnds(s), and backslash escapes
// skipped), or -1.
func mdFindCloser(s string, code []int, from int, c byte, n int) int {
	for j := from; j < len(s); {
		switch {
		case s[j] == '\\' && j+1 < len(s) && strings.IndexByte(mdEscapable, s[j+1]) >= 0:
			j += 2
		case s[j] == '`':
			if end := code[j]; end > 0 {
				j = end
			} else {
				j += mdRunLen(s, j, '`')
			}
		case j > from && mdIsMarker(s, j, c, n) && mdCanClose(s, j, c, n):
			return j
		default:
			j++
		}
	}
	return -1
}
