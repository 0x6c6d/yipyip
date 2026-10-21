package agent

import "strings"

// Markers around the current AGENTS.md content in the /init prompt.
const (
	initBeginMarker = "<<<BEGIN CURRENT AGENTS.md>>>"
	initEndMarker   = "<<<END CURRENT AGENTS.md>>>"
)

// InitPrompt returns the user message that makes the model inspect the
// repository and write AGENTS.md. existing is the current AGENTS.md content
// ("" if none).
func InitPrompt(existing string) string {
	var b strings.Builder
	b.WriteString("Inspect this repository and write " + AgentsFile + " in the start directory.\n")
	b.WriteString("First explore it with the glob, grep and read_file tools: read the README, the build files " +
		"(for example Makefile, go.mod, package.json), the test setup and the main source directories.\n")
	b.WriteString("Then write " + AgentsFile + " with the write_file tool (or edit_file when updating). " +
		"It is a short guide for coding agents working in this repository and must contain:\n")
	b.WriteString("- the build, test and lint commands (exact command lines),\n")
	b.WriteString("- architecture notes: the layout of the main directories and packages and how they fit together,\n")
	b.WriteString("- conventions and pitfalls that are not obvious from the code.\n")
	b.WriteString("Only state facts you verified in the repository; keep it concise.\n")
	if existing != "" {
		b.WriteString("\nAn " + AgentsFile + " already exists. Its current content is between the markers below.\n")
		b.WriteString("Update it instead of discarding it: keep everything that is still valid, " +
			"correct what is outdated and add what is missing.\n\n")
		b.WriteString(initBeginMarker + "\n")
		b.WriteString(existing)
		if !strings.HasSuffix(existing, "\n") {
			b.WriteString("\n")
		}
		b.WriteString(initEndMarker + "\n")
	}
	return b.String()
}

// Init starts a turn that makes the model write or update AGENTS.md in the
// start directory. It returns the errors of Submit (ErrNoModel, ErrBusy).
func (s *Session) Init() error {
	existing, _ := readAgentsFile(s.registry.Guard())
	return s.Submit(InitPrompt(s.Redact(existing)))
}
