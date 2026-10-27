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
)

// mdBlockRule tries to render the block starting at lines[i]. It returns the
// rendered lines, the index of the first line it did not consume and whether
// it applied at all. width is the available column count (<= 0: unknown).
type mdBlockRule func(lines []string, i, width int) (out []string, next int, ok bool)

// mdBlockRules are tried in order for every block; the last one always applies.
var mdBlockRules = []mdBlockRule{
	mdFenceRule,
	mdTableRule,
	mdHeadingRule,
	mdListRule,
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
	var cell strings.Builder
	closed := false
	split := func() {
		cells = append(cells, strings.TrimSpace(strings.ReplaceAll(cell.String(), `\|`, "|")))
		cell.Reset()
	}
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c == '`':
			n := mdRunLen(s, i, '`')
			end := mdCodeSpanEnd(s, i, n)
			if end < 0 {
				end = i + n
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
