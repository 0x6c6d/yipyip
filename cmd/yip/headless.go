package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/crashlog"
	"yipyip/internal/tools"
)

// maxHeadlessStdin caps the prompt read from stdin with -p -.
const maxHeadlessStdin = 10 << 20

// runHeadless runs one turn without a UI: it reads the prompt (from stdin
// for -p -), loads the configuration of the start directory, submits the
// prompt in auto mode and prints the main agent's final text on stdout.
// Progress goes to stderr, one redacted line per event. It returns 0, 1
// when the turn reported an error or the setup failed, 2 on a usage error.
func runHeadless(f flags, d deps, stdin io.Reader, stdout, stderr io.Writer, cs *crashState) int {
	prompt := f.h.prompt
	if prompt == "-" {
		if stdin == nil {
			stdin = strings.NewReader("")
		}
		data, err := io.ReadAll(io.LimitReader(stdin, maxHeadlessStdin))
		if err != nil {
			fmt.Fprintf(stderr, "yip: reading stdin: %v\n", err)
			return 1
		}
		prompt = string(data)
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		fmt.Fprintln(stderr, "yip: empty prompt")
		return 2
	}

	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "yip: %v\n", err)
		return 1
	}
	startDir, err := filepath.EvalSymlinks(wd)
	if err != nil {
		fmt.Fprintf(stderr, "yip: %v\n", err)
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "yip: %v\n", err)
		return 1
	}
	cfg, err := config.Load(home, startDir, os.Getenv)
	if err != nil {
		msg := err.Error()
		if cfg != nil && cfg.Redactor != nil {
			msg = cfg.Redactor.Redact(msg)
		}
		fmt.Fprintf(stderr, "yip: %s\n", msg)
		return 1
	}
	if guard, gerr := tools.NewPathGuard(startDir); gerr == nil {
		cs.logger = crashlog.New(crashlog.Options{
			Version:   version,
			Redact:    cfg.Redactor.Redact,
			Resolve:   guard.Resolve,
			SessionID: func() string { return "" },
		})
		crashlog.SetDefault(cs.logger)
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		go cs.logger.Watch(ctx, crashlog.WatchInterval)
	}
	sess, err := agent.NewSession(agent.Options{Config: cfg, StartDir: startDir})
	if err != nil {
		fmt.Fprintf(stderr, "yip: %s\n", cfg.Redactor.Redact(err.Error()))
		return 1
	}
	defer func() {
		sess.Cancel()
		sess.Wait()
	}()
	applyStoredModelEffort(cfg, sess)
	sess.SetMode(agent.ModeAuto)

	if strings.HasPrefix(prompt, "/") {
		word := strings.Fields(prompt)[0]
		fmt.Fprintf(stderr, "yip: %s is not available with -p\n", sess.Redact(word))
		return 2
	}
	if err := sess.Submit(prompt); err != nil {
		fmt.Fprintf(stderr, "yip: %s\n", sess.Redact(err.Error()))
		return 1
	}
	final, hadErr := consumeTurn(sess, stderr)
	fmt.Fprint(stdout, sess.Redact(final)+"\n")
	if hadErr {
		return 1
	}
	return 0
}

// applyStoredModelEffort applies the model and effort stored in the
// configuration (never the stored mode). Invalid values are ignored.
func applyStoredModelEffort(cfg *config.Config, sess *agent.Session) {
	if m, ok := cfg.Raw[config.SettingsKeyModel].(string); ok && m != "" && m != sess.ModelID() {
		_ = sess.SetModel(m)
	}
	if e, ok := cfg.Raw[config.SettingsKeyEffort].(string); ok {
		_ = sess.SetEffort(e)
	}
}

// consumeTurn reads the session's events until EvTurnDone. It returns the
// main agent's text of the last assistant message and whether an EvError
// occurred. Progress lines go to stderr, redacted.
func consumeTurn(sess *agent.Session, stderr io.Writer) (final string, hadErr bool) {
	var buf strings.Builder
	textSeen := false // main-agent text since the last main-agent tool call
	progress := func(line string) {
		fmt.Fprintln(stderr, sess.Redact(line))
	}
	ctx := context.Background()
	for {
		ev, err := sess.NextEvent(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return buf.String(), true
			}
			progress("error: " + err.Error())
			return buf.String(), true
		}
		main := ev.AgentID == agent.MainAgentID || ev.AgentID == ""
		switch ev.Kind {
		case agent.EvText:
			if main {
				buf.WriteString(ev.Text)
				textSeen = true
			}
		case agent.EvToolCall:
			if main && textSeen {
				buf.Reset()
				textSeen = false
			}
			progress("tool " + ev.Tool)
		case agent.EvSubagent:
			if !main {
				progress("subagent " + ev.Text)
			}
		case agent.EvRetry:
			progress("retry " + ev.Text)
		case agent.EvError:
			hadErr = true
			progress("error: " + ev.Text)
		case agent.EvApproval:
			if ev.Approval != nil {
				ev.Approval.Respond(agent.Deny)
			}
		case agent.EvQuestion:
			if ev.Question != nil {
				ev.Question.Cancel()
			}
		case agent.EvTurnDone:
			if main {
				return buf.String(), hadErr
			}
		}
	}
}
