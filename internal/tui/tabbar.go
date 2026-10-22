package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"yipyip/internal/session"
)

// tabBarRows is the height of the tab bar: the top line of the view.
const tabBarRows = 1

// newSessionTitle is the title of a tab whose conversation has no user
// message yet.
const newSessionTitle = "new session"

var (
	tabCurrentStyle = lipgloss.NewStyle().Bold(true)
	tabOtherStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

// tabTitle returns the title shown for tab i: the session title of its first
// user message (redacted), or newSessionTitle without one.
func (m *Model) tabTitle(i int) string {
	sess := m.tabAt(i).sess
	if sess == nil {
		return newSessionTitle
	}
	for _, h := range sess.History() {
		if h.Role != "user" {
			continue
		}
		if title := strings.TrimSpace(session.Title(sess.Redact(h.Content))); title != "" {
			return title
		}
		break
	}
	return newSessionTitle
}

// minTabTitleCells is the smallest title budget when titles are shortened to
// fit the bar.
const minTabTitleCells = 8

// tabZoneKind says what a click on a tabZone does.
type tabZoneKind int

const (
	tabZoneSwitch tabZoneKind = iota // the "<N>:<title>" or collapsed "…" part
	tabZoneClose                     // the "×" cell
	tabZonePlus                      // the "+" cell
)

// tabZone is the display columns [start, end) of one clickable part of the
// tab bar on screen row 0. tab is -1 for the plus zone.
type tabZone struct {
	start, end int
	tab        int
	kind       tabZoneKind
}

// barSeg is one tab segment of the bar: its label, followed by " ×" when
// close is set. A collapsed segment is a bare "…".
type barSeg struct {
	label string
	close bool
}

func (s barSeg) width() int {
	w := ansi.StringWidth(s.label)
	if s.close {
		w += 2
	}
	return w
}

// barWidth is the display width of the bar made of segs and, with plus, a
// trailing " +".
func barWidth(segs []barSeg, plus bool) int {
	w := len(segs) - 1
	for _, s := range segs {
		w += s.width()
	}
	if plus {
		w += 2
	}
	return w
}

// cutCells cuts s to at most n display cells, ending it with "…" if cut.
func cutCells(s string, n int) string {
	if ansi.StringWidth(s) <= n {
		return s
	}
	if n <= 0 {
		return ""
	}
	return ansi.Truncate(s, n, "…")
}

// tabBar renders the tab bar line of tabBarLayout. A single fresh tab still
// shows its one segment, so the bar never appears or disappears and the
// layout height stays fixed.
func (m Model) tabBar() string {
	line, _ := m.tabBarLayout()
	return line
}

// tabBarLayout is the only place the tab bar is laid out: one
// "<N>:<title> ×" segment per tab (the current one bold, the others dim),
// separated by one space, then " +" below maxTabs tabs. The line never
// exceeds the window width: titles are shortened to a common budget, then
// segments left and right of the current tab collapse to "…", then the "+"
// goes, then the current title is cut. zones are the clickable display
// columns of the line, in order.
func (m Model) tabBarLayout() (line string, zones []tabZone) {
	titles := make([]string, len(m.tabs))
	maxTitle := 0
	for i := range m.tabs {
		titles[i] = m.tabTitle(i)
		maxTitle = max(maxTitle, ansi.StringWidth(titles[i]))
	}
	segs := func(budget int) []barSeg {
		out := make([]barSeg, len(titles))
		for i, t := range titles {
			out[i] = barSeg{label: strconv.Itoa(i+1) + ":" + cutCells(t, budget), close: true}
		}
		return out
	}
	plus := len(m.tabs) < maxTabs
	cur := segs(maxTitle)
	if m.width > 0 && barWidth(cur, plus) > m.width {
		for b := maxTitle - 1; b >= minTabTitleCells; b-- {
			cur = segs(b)
			if barWidth(cur, plus) <= m.width {
				break
			}
		}
		if barWidth(cur, plus) > m.width {
			cur = segs(minTabTitleCells)
		}
		collapse := func(i int) {
			if barWidth(cur, plus) > m.width {
				cur[i] = barSeg{label: "…"}
			}
		}
		for i := 0; i < m.cur; i++ {
			collapse(i)
		}
		for i := len(cur) - 1; i > m.cur; i-- {
			collapse(i)
		}
		if barWidth(cur, plus) > m.width {
			plus = false
		}
		if over := barWidth(cur, plus) - m.width; over > 0 {
			num := strconv.Itoa(m.cur+1) + ":"
			title := ansi.StringWidth(cur[m.cur].label) - len(num)
			if title-over >= 0 {
				cur[m.cur].label = num + cutCells(titles[m.cur], title-over)
			} else {
				// Not even "<N>: ×" fits: keep as much of "<N>:" as possible.
				cur[m.cur] = barSeg{label: cutCells(num, ansi.StringWidth(num)-(over-title-2))}
			}
		}
	}

	var sb strings.Builder
	col := 0
	for i, s := range cur {
		if i > 0 {
			sb.WriteByte(' ')
			col++
		}
		text := s.label
		if s.close {
			text += " ×"
		}
		style := tabOtherStyle
		if i == m.cur {
			style = tabCurrentStyle
		}
		sb.WriteString(style.Render(text))
		lw := ansi.StringWidth(s.label)
		zones = append(zones, tabZone{start: col, end: col + lw, tab: i, kind: tabZoneSwitch})
		if s.close {
			zones = append(zones, tabZone{start: col + lw + 1, end: col + lw + 2, tab: i, kind: tabZoneClose})
		}
		col += s.width()
	}
	if plus {
		sb.WriteByte(' ')
		sb.WriteString(tabOtherStyle.Render("+"))
		zones = append(zones, tabZone{start: col + 1, end: col + 2, tab: -1, kind: tabZonePlus})
	}
	return sb.String(), zones
}
