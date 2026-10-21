package tui

import (
	"io"
	"os"

	"github.com/atotto/clipboard"
	osc52 "github.com/aymanbagabas/go-osc52/v2"
)

// writeClipboard copies text to the clipboard. It always writes the OSC 52
// sequence to w in a single Write, so terminals that support it (also over
// SSH) receive the text. When useTool is true it also hands the text to
// atotto/clipboard, which runs only its fixed wl-copy / xclip / xsel binaries
// and passes the text on stdin. Errors are ignored: the caller shows its
// notice anyway. Callers pass text that is already redacted.
func writeClipboard(w io.Writer, useTool bool, text string) error {
	_, _ = io.WriteString(w, osc52.New(text).String())
	if useTool {
		_ = clipboard.WriteAll(text)
	}
	return nil
}

// defaultClipboard is the production clipboard writer. stderr is the
// terminal; stdout belongs to the renderer.
func defaultClipboard(text string) error {
	return writeClipboard(os.Stderr, !clipboard.Unsupported, text)
}
