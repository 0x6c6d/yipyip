package tui

import (
	"reflect"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// keyFromCSI converts the unknown CSI sequences terminals send for modified
// keys into the key messages the rest of the model handles. bubbletea v1
// reports such a sequence as its unexported unknownCSISequenceMsg (a []byte),
// which is recognised by type name. The forms understood are xterm
// modifyOtherKeys (ESC [ 27 ; mod ; code ~) and, for Shift+Enter, CSI-u
// (ESC [ 13 ; 2 u). Shift+Enter becomes the key named "shift+enter" that the
// input's newline binding knows. Anything else is not converted (ok false).
func keyFromCSI(msg tea.Msg) (tea.KeyMsg, bool) {
	v := reflect.ValueOf(msg)
	if !v.IsValid() || v.Kind() != reflect.Slice || v.Type().Elem().Kind() != reflect.Uint8 ||
		v.Type().Name() != "unknownCSISequenceMsg" {
		return tea.KeyMsg{}, false
	}
	seq := string(v.Bytes())
	if !strings.HasPrefix(seq, "\x1b[") {
		return tea.KeyMsg{}, false
	}
	body := seq[2:]
	switch body {
	case "13;2u":
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shift+enter")}, true
	}
	rest, ok := strings.CutPrefix(body, "27;")
	if !ok {
		return tea.KeyMsg{}, false
	}
	rest, ok = strings.CutSuffix(rest, "~")
	if !ok {
		return tea.KeyMsg{}, false
	}
	modS, codeS, ok := strings.Cut(rest, ";")
	if !ok {
		return tea.KeyMsg{}, false
	}
	mod, err1 := strconv.Atoi(modS)
	code, err2 := strconv.Atoi(codeS)
	if err1 != nil || err2 != nil {
		return tea.KeyMsg{}, false
	}
	switch {
	case mod == 2 && code == 13:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("shift+enter")}, true
	case mod == 3 && code == 13:
		return tea.KeyMsg{Type: tea.KeyEnter, Alt: true}, true
	case mod == 5 && code >= 'a' && code <= 'z':
		return tea.KeyMsg{Type: tea.KeyCtrlA + tea.KeyType(code-'a')}, true
	}
	return tea.KeyMsg{}, false
}
