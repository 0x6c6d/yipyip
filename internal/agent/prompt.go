package agent

import (
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"yipyip/internal/config"
	"yipyip/internal/tools"
)

// AgentsFile is the project instructions file appended to the system prompt.
const AgentsFile = "AGENTS.md"

// maxAgentsChars bounds the AGENTS.md content included in the system prompt.
const maxAgentsChars = 30000

const basePrompt = `You are yip, a terminal coding assistant working in the directory %s (the start directory).
You can read, search, create and edit files and run shell commands with the tools provided. You have no web access.
All paths are relative to the start directory; you cannot access files outside it.
Read files before editing them, make focused changes, and verify your work when you can.
Be concise: answer directly, without filler.`

// buildSystemPrompt returns the system prompt for startDir (already
// symlink-resolved), including AGENTS.md when present. The AGENTS.md content
// passes through red.
func buildSystemPrompt(startDir string, guard *tools.PathGuard, red *config.Redactor) string {
	p := strings.Replace(basePrompt, "%s", startDir, 1)
	if notes, ok := readAgentsFile(guard); ok {
		p += "\n\n# AGENTS.md\n" + red.Redact(notes)
	}
	return p
}

// readAgentsFile reads AGENTS.md from the start directory, truncated to
// maxAgentsChars characters. It only reads a regular file inside the start
// directory.
func readAgentsFile(guard *tools.PathGuard) (string, bool) {
	if guard == nil {
		return "", false
	}
	path, err := guard.Resolve(AgentsFile)
	if err != nil {
		return "", false
	}
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		return "", false
	}
	// A character is at most utf8.UTFMax bytes.
	data, err := io.ReadAll(io.LimitReader(f, int64(maxAgentsChars*utf8.UTFMax)))
	if err != nil {
		return "", false
	}
	return truncateChars(strings.ToValidUTF8(string(data), "�"), maxAgentsChars), true
}

// truncateChars returns at most n characters (runes) of s.
func truncateChars(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos]
		}
		i++
	}
	return s
}
