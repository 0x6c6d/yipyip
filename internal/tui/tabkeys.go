package tui

// keyCycleBack is the key string of the backward tab cycle (Ctrl+Shift+Tab;
// bubbletea has no key type for it, so it is matched by its String()). It is
// the only place the string appears; plain Shift+Tab stays the mode toggle.
const keyCycleBack = "ctrl+shift+tab"
