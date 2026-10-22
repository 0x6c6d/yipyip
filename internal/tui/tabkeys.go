package tui

import "strings"

// ignoredTabKey reports whether s is a key string that once switched tabs
// (Ctrl+Tab, Ctrl+Shift+Tab, Alt+1..Alt+8) and is now swallowed. Bubbletea has
// no key type for the Ctrl+Tab pair, so they are matched by String(). Plain
// Shift+Tab stays the mode toggle.
func ignoredTabKey(s string) bool {
	if s == "ctrl+tab" || s == "ctrl+shift+tab" {
		return true
	}
	d, ok := strings.CutPrefix(s, "alt+")
	return ok && len(d) == 1 && d[0] >= '1' && d[0] <= '8'
}
