package tui

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// longTitleModel returns a model with n tabs, each with a 60-rune title.
func longTitleModel(t *testing.T, n int) Model {
	t.Helper()
	m, _ := newSessionModel(t, answerServer(t, "ok"))
	for i := 0; i < n; i++ {
		if i > 0 {
			mustAddTab(t, &m)
		}
		m = tabTurn(t, m, strings.Repeat(string(rune('a'+i)), 60))
	}
	return m
}

func TestTabBarBounds(t *testing.T) {
	base := longTitleModel(t, 8)
	for _, width := range []int{60, 120} {
		for _, cur := range []int{1, 4, 8} {
			m := base
			m.cur = cur - 1
			next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
			m = next.(Model)
			view := m.View()
			first, _, _ := strings.Cut(view, "\n")
			plain := tabBarLine(m)
			if w := ansi.StringWidth(first); w > width {
				t.Errorf("width %d cur %d: bar is %d cells", width, cur, w)
			}
			if !strings.Contains(plain, strconv.Itoa(cur)+":") {
				t.Errorf("width %d cur %d: current number missing: %q", width, cur, plain)
			}
			if !strings.Contains(plain, "×") {
				t.Errorf("width %d cur %d: no ×: %q", width, cur, plain)
			}
			if strings.Contains(plain, "+") {
				t.Errorf("width %d cur %d: + drawn at 8 tabs: %q", width, cur, plain)
			}
			if h := lipgloss.Height(view); h != 40 {
				t.Errorf("width %d cur %d: view height %d, want 40", width, cur, h)
			}
			if width == 120 {
				for n := 1; n <= 8; n++ {
					if !strings.Contains(plain, strconv.Itoa(n)+":") {
						t.Errorf("width 120 cur %d: %d: missing: %q", cur, n, plain)
					}
				}
			} else if !strings.Contains(plain, "…") {
				t.Errorf("width 60 cur %d: no collapsed segment: %q", cur, plain)
			}
		}
	}
}

func TestTabBarZones(t *testing.T) {
	m, _ := newSessionModel(t, nil)
	mustAddTab(t, &m)
	mustAddTab(t, &m)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = next.(Model)
	line, zones := m.tabBarLayout()
	cells := []rune(strings.Repeat(" ", 160))
	_ = line
	plain := []rune(tabBarLine(m))
	copy(cells, plain)
	end, closes, plus := 0, 0, 0
	for _, z := range zones {
		if z.start < end || z.end <= z.start {
			t.Fatalf("zones overlap or are empty: %+v", zones)
		}
		end = z.end
		switch z.kind {
		case tabZoneClose:
			if cells[z.start] != '×' || z.end-z.start != 1 || z.tab != closes {
				t.Errorf("close zone %+v does not cover the × of tab %d in %q", z, closes, string(plain))
			}
			closes++
		case tabZonePlus:
			if cells[z.start] != '+' || z.tab != -1 {
				t.Errorf("plus zone %+v does not cover the + in %q", z, string(plain))
			}
			plus++
		}
	}
	if closes != 3 || plus != 1 {
		t.Errorf("got %d close and %d plus zones, want 3 and 1", closes, plus)
	}

	for i := 3; i < maxTabs; i++ {
		mustAddTab(t, &m)
	}
	_, zones = m.tabBarLayout()
	for _, z := range zones {
		if z.kind == tabZonePlus {
			t.Errorf("plus zone with %d tabs", maxTabs)
		}
	}
}

func TestTabBarFixedWidth(t *testing.T) {
	m, _ := newSessionModel(t, answerServer(t, "ok"))
	m = tabTurn(t, m, "hi")
	mustAddTab(t, &m)
	m = tabTurn(t, m, strings.Repeat("long ", 20))
	mustAddTab(t, &m)
	m = tabTurn(t, m, "日本語のとても長いタイトルです日本語")
	mustAddTab(t, &m) // empty: new session
	check := func(width int, allowCollapsed bool) {
		next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
		mm := next.(Model)
		line, zones := mm.tabBarLayout()
		plain := ansi.Strip(line)
		if w := ansi.StringWidth(plain); mm.width > 0 && w > mm.width {
			t.Errorf("width %d: bar is %d cells", width, w)
		}
		sw := -1
		for _, z := range zones {
			if z.kind != tabZoneSwitch {
				continue
			}
			if allowCollapsed && z.end-z.start == 1 {
				continue
			}
			w := z.end - z.start
			if sw >= 0 && w != sw {
				t.Errorf("width %d: label widths differ: %d vs %d in %q", width, w, sw, plain)
			}
			sw = w
		}
		for _, z := range zones {
			if z.kind == tabZoneClose {
				if got := ansi.Cut(plain, z.start, z.end); got != "×" {
					t.Errorf("width %d: close zone of tab %d is %q", width, z.tab, got)
				}
				if z.end-z.start != 1 {
					t.Errorf("close zone width %d", z.end-z.start)
				}
			}
			if z.kind == tabZonePlus {
				if got := ansi.Cut(plain, z.start, z.end); got != "+" {
					t.Errorf("plus zone is %q", got)
				}
			}
		}
		if !allowCollapsed {
			if sw != fixedTabTitleCells+2 {
				t.Errorf("label width %d, want %d", sw, fixedTabTitleCells+2)
			}
		}
	}
	check(200, false)
	check(50, true)
}
