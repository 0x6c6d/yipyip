package tui

import (
	"strings"

	"yipyip/internal/config"
)

// Settings persistence: every successful change of the global model, effort
// or mode selection writes the user config through config.SaveSettings
// (Options.SaveSettings in tests). Only the three plain strings of the
// selection reach the saver, never a config object, so no resolved secret
// can be written.
//
// The write is synchronous, at a defined edge: right after the change has
// been applied and confirmed in the conversation, inside the same Update /
// Command call. The file is a few hundred bytes, so the cost is negligible,
// and a failure reaches the view deterministically without an extra
// message round trip. A failure appends exactly one info line to the
// current tab's conversation; the change stays applied, nothing is retried.

// settingsFailPrefix starts the one failure line.
const settingsFailPrefix = "could not save settings: "

// globalSel is the global model, effort and mode at one moment.
type globalSel struct{ model, effort, mode string }

// currentSel returns the current global selection.
func (m *Model) currentSel() globalSel {
	model, effort, mode := m.globals()
	return globalSel{model, effort, mode}
}

// saveSettingsIfChanged writes the settings when the global selection now
// differs from prev (the selection before the change); a no-op change
// writes nothing.
func (m *Model) saveSettingsIfChanged(prev globalSel) {
	if m.currentSel() != prev {
		m.saveSettings()
	}
}

// saveSettings writes the current global selection to Options.SettingsPath
// ("" = disabled). A failure adds one info line `could not save settings:
// <err>` to the current tab's conversation.
func (m *Model) saveSettings() {
	path := m.opts.SettingsPath
	if path == "" {
		return
	}
	save := m.opts.SaveSettings
	if save == nil {
		save = config.SaveSettings
	}
	model, effort, mode := m.globals()
	if err := save(path, model, effort, mode); err != nil {
		// config.SaveSettings already prefixes its errors; keep one prefix.
		msg := strings.TrimPrefix(err.Error(), settingsFailPrefix)
		msg = strings.Join(strings.Fields(msg), " ") // one line only
		m.add(blockInfo, m.redact(settingsFailPrefix+msg))
	}
}
