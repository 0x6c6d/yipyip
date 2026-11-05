package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"yipyip/internal/agent"
	"yipyip/internal/config"
	"yipyip/internal/crashlog"
	"yipyip/internal/session"
	"yipyip/internal/skills"
	"yipyip/internal/tools"
	"yipyip/internal/tui"
)

// maxHeadlessStdin caps the prompt read from stdin with -p -.
const maxHeadlessStdin = 10 << 20

// headlessTruncate is the maximum number of runes of a tool_result output in
// the --json trace.
const headlessTruncate = 2000

// headlessGrace bounds the wait for a cancelled turn (timeout, signal) to end.
const headlessGrace = 10 * time.Second

// runHeadless runs one turn without a UI: it reads the prompt (from stdin
// for -p -), loads the configuration of the start directory, submits the
// prompt in auto mode and prints the main agent's final text on stdout.
// Progress goes to stderr, one redacted line per event. With --json, stdout
// gets one JSON record per line instead (the last one a result record) and
// stderr stays empty except for fatal errors. It returns 0, 1 when the turn
// reported an error, the setup failed or a signal interrupted it, 2 on a
// usage error, 3 on an unanswered question, 4 when --timeout expired and 5
// when --max-model-calls was used up (see exitFor).
func runHeadless(f flags, d deps, stdin io.Reader, stdout, stderr io.Writer, cs *crashState) int {
	r := &runner{
		json:    f.h.json,
		answers: f.h.answers,
		approve: f.h.approve,
		stdout:  stdout,
		stderr:  stderr,
		start:   time.Now(),
		redact:  func(s string) string { return s },
		tools:   map[string]int{},
		files:   map[string]bool{},
		subs:    map[string]bool{},
		calls:   map[string]string{},
	}
	code := r.run(f, stdin, cs)
	r.saveSession(f, code)
	r.writeResult(code)
	if r.tfile != nil {
		if err := r.tfile.Close(); err != nil && code == 0 {
			fmt.Fprintf(stderr, "yip: closing transcript: %s\n", r.redact(err.Error()))
			code = 1
		}
	}
	return code
}

// runner holds the state of one headless run.
type runner struct {
	json           bool
	stdout, stderr io.Writer
	start          time.Time
	redact         func(string) string // session redactor once known
	sess           *agent.Session
	startDir       string
	m              mapper
	tm             mapper   // transcript mapper (tool output untruncated)
	tfile          *os.File // --transcript file, nil without it

	final    strings.Builder // main-agent text of the last assistant message
	textSeen bool            // main-agent text since the last main-agent tool call
	hadErr   bool
	reason   string // result reason decided by run ("" = derive from the code)

	timedOut    atomic.Bool // --timeout expired and cancelled the turn
	interrupted atomic.Bool // SIGINT/SIGTERM cancelled the turn
	limitHit    bool        // the main agent's turn ended on the model call limit

	approve    []string // --approve tool names ("all" approves every gated call)
	answers    []string // --answer values, in order
	asked      int      // questions seen in this run (main agent and subagents)
	unanswered bool     // a question beyond the answers cancelled the turn

	tools map[string]int    // tool_call count per tool name
	files map[string]bool   // start-dir-relative paths changed by write_file/edit_file
	subs  map[string]bool   // distinct subagent ids
	calls map[string]string // agent id -> JSON args of its pending write_file/edit_file call

	pendAgent string          // agent of the buffered text deltas
	pendText  strings.Builder // buffered text deltas (--json)

	resumed   *session.SessionData // the session loaded by --resume, nil without it
	sessionID string               // id of the saved session ("" = not saved)
}

// run performs the setup and the turn and returns the exit code.
func (r *runner) run(f flags, stdin io.Reader, cs *crashState) int {
	prompt := f.h.prompt
	if prompt == "-" {
		if stdin == nil {
			stdin = strings.NewReader("")
		}
		data, err := io.ReadAll(io.LimitReader(stdin, maxHeadlessStdin))
		if err != nil {
			fmt.Fprintf(r.stderr, "yip: reading stdin: %v\n", err)
			return 1
		}
		prompt = string(data)
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		fmt.Fprintln(r.stderr, "yip: empty prompt")
		return 2
	}

	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(r.stderr, "yip: %v\n", err)
		return 1
	}
	startDir, err := filepath.EvalSymlinks(wd)
	if err != nil {
		fmt.Fprintf(r.stderr, "yip: %v\n", err)
		return 1
	}
	r.startDir = startDir
	guard, gerr := tools.NewPathGuard(startDir)
	if f.h.transcript != "" {
		var tpath string
		if gerr == nil {
			tpath, gerr = guard.Resolve(f.h.transcript)
		}
		if gerr != nil {
			fmt.Fprintln(r.stderr, "yip: transcript path outside the start directory")
			return 2
		}
		tf, err := os.OpenFile(tpath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			fmt.Fprintln(r.stderr, "yip: cannot create the transcript file")
			return 1
		}
		r.tfile = tf
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(r.stderr, "yip: %v\n", err)
		return 1
	}
	cfg, err := config.Load(home, startDir, os.Getenv)
	if cfg != nil && cfg.Redactor != nil {
		r.redact = cfg.Redactor.Redact
	}
	if err != nil {
		fmt.Fprintf(r.stderr, "yip: %s\n", r.redact(err.Error()))
		return 1
	}
	if gerr == nil {
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
	sess, err := agent.NewSession(agent.Options{Config: cfg, StartDir: startDir, MaxModelCalls: f.h.maxCalls})
	if err != nil {
		fmt.Fprintf(r.stderr, "yip: %s\n", r.redact(err.Error()))
		return 1
	}
	defer func() {
		sess.Cancel()
		waitBounded(sess, headlessGrace)
	}()
	r.sess = sess
	r.redact = sess.Redact
	r.m = mapper{redact: sess.Redact, truncate: headlessTruncate, sub: r.subLookup}
	r.tm = mapper{redact: sess.Redact, truncate: 0, sub: r.subLookup}
	applyStoredModelEffort(cfg, sess)
	if f.resume {
		if code := r.resume(f, sess); code != 0 {
			return code
		}
	}
	if f.h.mode == "manual" {
		sess.SetMode(agent.ModeManual)
	} else {
		sess.SetMode(agent.ModeAuto)
	}
	known := map[string]bool{"all": true}
	for _, n := range sess.ToolNames() {
		known[n] = true
	}
	for _, n := range r.approve {
		if !known[n] {
			fmt.Fprintf(r.stderr, "yip: unknown tool %s in --approve\n", r.redact(n))
			return 2
		}
	}

	if strings.HasPrefix(prompt, "/") {
		fields := strings.Fields(prompt)
		word := fields[0]
		name := strings.TrimPrefix(word, "/")
		var found *skills.Skill
		sks, warns := skills.Load(home, startDir)
		for _, w := range warns {
			fmt.Fprintf(r.stderr, "warning: %s\n", r.redact(w))
		}
		if !tui.IsBuiltinCommand(name) {
			for i := range sks {
				if sks[i].Name == name {
					found = &sks[i]
					break
				}
			}
		}
		if found == nil {
			fmt.Fprintf(r.stderr, "yip: %s is not available with -p\n", r.redact(word))
			return 2
		}
		msg := found.Body
		if args := strings.TrimSpace(strings.TrimPrefix(prompt, word)); args != "" {
			msg += "\n\n" + args
		}
		prompt = sess.Redact(msg)
	}
	sigCtx, stopSig := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSig()
	// evCtx bounds the event loop: it ends headlessGrace after a timeout or a
	// signal cancelled the turn, so a stuck turn never blocks the run.
	evCtx, stopEv := context.WithCancel(context.Background())
	defer stopEv()
	cancelled := make(chan struct{}, 1)
	trigger := func(flag *atomic.Bool) {
		if flag.CompareAndSwap(false, true) {
			sess.Cancel()
			select {
			case cancelled <- struct{}{}:
			default:
			}
		}
	}
	var timeoutC <-chan time.Time
	if f.h.timeout > 0 {
		timer := time.NewTimer(f.h.timeout)
		defer timer.Stop()
		timeoutC = timer.C
	}
	finished := make(chan struct{})
	defer close(finished)
	if err := sess.Submit(prompt); err != nil {
		fmt.Fprintf(r.stderr, "yip: %s\n", r.redact(err.Error()))
		return 1
	}
	go func() {
		select {
		case <-timeoutC:
			trigger(&r.timedOut)
		case <-sigCtx.Done():
			trigger(&r.interrupted)
		case <-finished:
			return
		}
		select {
		case <-time.After(headlessGrace):
			stopEv()
		case <-finished:
		}
	}()
	r.consumeTurn(evCtx)
	if r.unanswered || r.timedOut.Load() || r.interrupted.Load() {
		waitBounded(sess, headlessGrace)
	}
	maxCalls := r.limitHit && f.h.maxCalls > 0 && sess.ModelCalls() >= f.h.maxCalls
	if maxCalls {
		sess.Cancel()
	}
	if !r.json {
		fmt.Fprint(r.stdout, r.redact(r.final.String())+"\n")
	}
	code, reason := exitFor(exitState{
		timeout:  r.timedOut.Load(),
		question: r.unanswered,
		maxCalls: maxCalls,
		err:      r.hadErr || r.interrupted.Load(),
	})
	r.reason = reason
	switch reason {
	case "timeout":
		fmt.Fprintln(r.stderr, "yip: timeout")
	case "question":
		fmt.Fprintln(r.stderr, "yip: unanswered question")
	case "max_calls":
		fmt.Fprintln(r.stderr, "yip: model call limit reached")
	default:
		if r.interrupted.Load() {
			fmt.Fprintln(r.stderr, "yip: interrupted")
		}
	}
	return code
}

// resume loads the session of --resume <id> before any model call and
// applies its conversation, tokens and (unless --model/--effort are given)
// its model and effort to sess. A load error prints only the id (the error
// names a path) and returns 1.
func (r *runner) resume(f flags, sess *agent.Session) int {
	var data *session.SessionData
	store, err := session.NewStore(r.startDir)
	if err == nil {
		data, err = store.Load(f.resumeID)
	}
	if err != nil {
		fmt.Fprintf(r.stderr, "unknown session %s\n", r.redact(f.resumeID))
		return 1
	}
	session.ApplyTo(data, sess)
	if f.h.model == "" && data.Model != "" && data.Model != sess.ModelID() {
		_ = sess.SetModel(data.Model)
	}
	if f.h.effort == "" && data.Effort != "" {
		_ = sess.SetEffort(data.Effort)
	}
	r.resumed = data
	return 0
}

// saveSession saves the conversation once at the end of the run (exit codes
// 0, 1, 3, 4 and 5; never 2, never with --no-save), after the turn ended,
// then prunes the sessions directory to the newest keepSessions, protecting
// the saved one. A resumed run keeps the loaded id and created time; a new
// run lets the store derive the id. A save error is reported on stderr and
// leaves the exit code unchanged.
func (r *runner) saveSession(f flags, code int) {
	if f.h.noSave || r.sess == nil {
		return
	}
	switch code {
	case 0, 1, 3, 4, 5:
	default:
		return
	}
	waitBounded(r.sess, headlessGrace)
	id, created := "", r.start.UTC().Format(time.RFC3339)
	if r.resumed != nil {
		id, created = f.resumeID, r.resumed.Created
	}
	data, ok := session.FromAgent(r.sess, id, created)
	if !ok {
		return
	}
	store, err := session.NewStore(r.startDir)
	if err == nil {
		err = store.Save(data)
	}
	if err != nil {
		fmt.Fprintf(r.stderr, "yip: could not save session: %s\n", r.redact(err.Error()))
		return
	}
	r.sessionID = data.ID
	_ = store.Prune(keepSessions, data.ID)
}

// exitState is what decides the exit code of a finished headless turn.
type exitState struct {
	timeout  bool // --timeout expired
	question bool // a question beyond the --answer values
	maxCalls bool // --max-model-calls used up
	err      bool // an error or a signal
}

// exitFor returns the exit code and result reason of s. Precedence: timeout
// (4) > question (3) > max_calls (5) > error (1) > done (0).
func exitFor(s exitState) (code int, reason string) {
	switch {
	case s.timeout:
		return 4, "timeout"
	case s.question:
		return 3, "question"
	case s.maxCalls:
		return 5, "max_calls"
	case s.err:
		return 1, "error"
	}
	return 0, "done"
}

// waitBounded waits for the running turn of sess to end, at most d.
func waitBounded(sess *agent.Session, d time.Duration) {
	done := make(chan struct{})
	go func() {
		sess.Wait()
		close(done)
	}()
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
	}
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

// subLookup returns the task and model of subagent id.
func (r *runner) subLookup(id string) (task, model string) {
	for _, s := range r.sess.Subagents() {
		if s.ID == id {
			return s.Task, s.Model
		}
	}
	return "", ""
}

// progress writes one redacted progress line to stderr (not with --json).
func (r *runner) progress(line string) {
	if !r.json {
		fmt.Fprintln(r.stderr, r.redact(line))
	}
}

// emit writes records to stdout with --json and to the transcript file with
// --transcript. Approval and result records are redacted by the caller.
func (r *runner) emit(recs ...any) {
	for _, rec := range recs {
		if r.json {
			_ = encodeLine(r.stdout, rec)
		}
		if r.tfile != nil {
			_ = encodeLine(r.tfile, rec)
		}
	}
}

// emitEvent maps ev and writes its records: the truncating mapper for the
// --json stdout, the untruncating one for the transcript. Both redact.
func (r *runner) emitEvent(ev agent.Event) {
	if r.json {
		for _, rec := range r.m.records(ev) {
			_ = encodeLine(r.stdout, rec)
		}
	}
	if r.tfile != nil {
		for _, rec := range r.tm.records(ev) {
			_ = encodeLine(r.tfile, rec)
		}
	}
}

// flushText writes the buffered text deltas as one text record.
func (r *runner) flushText() {
	if r.pendText.Len() == 0 {
		return
	}
	ev := agent.Event{Kind: agent.EvText, AgentID: r.pendAgent, Text: r.pendText.String()}
	r.pendText.Reset()
	r.emitEvent(ev)
}

// consumeTurn reads the session's events until the main agent's
// EvTurnDone (or until ctx is done), collecting the final text, counts and
// changed files.
func (r *runner) consumeTurn(ctx context.Context) {
	for {
		ev, err := r.sess.NextEvent(ctx)
		if err != nil {
			r.flushText()
			r.hadErr = true
			if !errors.Is(err, context.Canceled) {
				r.progress("error: " + err.Error())
				r.emitEvent(agent.Event{Kind: agent.EvError, AgentID: agent.MainAgentID, Text: err.Error()})
			}
			return
		}
		if ev.Kind == agent.EvText {
			if ev.AgentID != r.pendAgent {
				r.flushText()
			}
			r.pendAgent = ev.AgentID
			r.pendText.WriteString(ev.Text)
		} else {
			r.flushText()
		}
		if r.handle(ev) {
			return
		}
	}
}

// handle processes one event and reports whether the turn is done.
func (r *runner) handle(ev agent.Event) bool {
	main := ev.AgentID == agent.MainAgentID || ev.AgentID == ""
	switch ev.Kind {
	case agent.EvText:
		if main {
			r.final.WriteString(ev.Text)
			r.textSeen = true
		}
	case agent.EvToolCall:
		if main && r.textSeen {
			r.final.Reset()
			r.textSeen = false
		}
		r.tools[ev.Tool]++
		if ev.Tool == "write_file" || ev.Tool == "edit_file" {
			r.calls[ev.AgentID] = ev.Args
		} else {
			delete(r.calls, ev.AgentID)
		}
		r.progress("tool " + ev.Tool)
		r.emitEvent(ev)
	case agent.EvToolResult:
		if args, ok := r.calls[ev.AgentID]; ok {
			delete(r.calls, ev.AgentID)
			if !ev.IsError && (ev.Tool == "write_file" || ev.Tool == "edit_file") {
				r.addFile(args)
			}
		}
		r.emitEvent(ev)
	case agent.EvSubagent:
		if !main {
			r.subs[ev.AgentID] = true
			r.progress("subagent " + ev.Text)
			r.emitEvent(ev)
		}
	case agent.EvRetry:
		r.progress("retry " + ev.Text)
		r.emitEvent(ev)
	case agent.EvError:
		r.hadErr = true
		if main && strings.Contains(ev.Text, agent.ErrModelCallLimit.Error()) {
			r.limitHit = true
		}
		r.progress("error: " + ev.Text)
		r.emitEvent(ev)
	case agent.EvApproval:
		if ev.Approval != nil {
			r.approval(ev)
		}
	case agent.EvQuestion:
		r.question(ev)
	case agent.EvTurnDone:
		if main {
			return true
		}
	}
	return false
}

// approval answers a gated tool call: Allow when --approve names the tool or
// "all", Deny otherwise (never the session-wide grant). It writes one approval record,
// or a stderr line without --json.
func (r *runner) approval(ev agent.Event) {
	a := ev.Approval
	ok := false
	for _, n := range r.approve {
		if n == "all" || n == a.Tool {
			ok = true
			break
		}
	}
	if ok {
		a.Respond(agent.Allow)
	} else {
		a.Respond(agent.Deny)
	}
	agentID := a.AgentID
	if agentID == "" {
		agentID = ev.AgentID
	}
	decision := "denied"
	if ok {
		decision = "approved"
	}
	r.progress("approval " + a.Tool + " " + decision)
	r.emit(approvalRecord(r.redact(agentID), r.redact(a.Tool), approvalSummary(r.redact, a.Tool, a.Args), ok))
}

// approvalSummary returns the redacted, single-line summary (at most 200
// runes) of a gated call: the command of bash, the url of web_fetch, the
// query of web_search, the path of file tools, else the raw arguments.
func approvalSummary(redact func(string) string, tool, args string) string {
	key := ""
	switch tool {
	case "bash":
		key = "command"
	case "web_fetch":
		key = "url"
	case "web_search":
		key = "query"
	case "write_file", "edit_file", "read_file":
		key = "path"
	}
	s := args
	if key != "" {
		var m map[string]any
		if json.Unmarshal([]byte(args), &m) == nil {
			if v, ok := m[key].(string); ok {
				s = v
			}
		}
	}
	s = redact(s)
	s = strings.Join(strings.Fields(s), " ")
	s = redact(s)
	if rs := []rune(s); len(rs) > 200 {
		s = string(rs[:200])
	}
	return s
}

// question emits a question record (or a stderr line) and answers the n-th
// question of the run with the n-th --answer. A question beyond the answers,
// or one arriving after that, cancels the turn (and its subagents) and is
// itself cancelled; the run then ends with exit code 3.
func (r *runner) question(ev agent.Event) {
	r.emitEvent(ev)
	text := ev.Text
	if ev.Question != nil {
		text = ev.Question.Question
	}
	r.progress("question: " + text)
	if ev.Question == nil {
		return
	}
	r.asked++
	if !r.unanswered && r.asked <= len(r.answers) {
		ev.Question.Respond(r.answers[r.asked-1])
		return
	}
	if !r.unanswered {
		r.unanswered = true
		// Cancel the turn first so that the cancelled answer never leads to
		// another model request.
		r.sess.Cancel()
	}
	ev.Question.Cancel()
}

// addFile records the path argument of a successful write_file/edit_file
// call, relative to the start directory.
func (r *runner) addFile(args string) {
	var a struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil || a.Path == "" {
		return
	}
	p := filepath.Clean(a.Path)
	if filepath.IsAbs(p) {
		rel, err := filepath.Rel(r.startDir, p)
		if err != nil {
			return
		}
		p = rel
	}
	r.files[filepath.ToSlash(p)] = true
}

// writeResult writes the result record with --json and to the transcript;
// every string passes the redactor.
func (r *runner) writeResult(code int) {
	if !r.json && r.tfile == nil {
		return
	}
	reason := r.reason
	switch {
	case reason != "":
	case code == 3 && r.unanswered:
		reason = "question"
	case code == 2:
		reason = "usage"
	case code != 0:
		reason = "error"
	default:
		reason = "done"
	}
	rec := resultRecord{
		Type:         "result",
		ExitCode:     code,
		Reason:       reason,
		FinalText:    r.redact(r.final.String()),
		Subagents:    len(r.subs),
		DurationMS:   time.Since(r.start).Milliseconds(),
		Tools:        map[string]int{},
		FilesChanged: []string{},
	}
	rec.SessionID = r.redact(r.sessionID)
	if r.sess != nil {
		rec.ModelCalls = r.sess.ModelCalls()
		in, out, _ := r.sess.TokenCounts()
		rec.Tokens = tokensRecord{In: int(in), Out: int(out)}
	}
	for name, n := range r.tools {
		rec.Tools[r.redact(name)] += n
	}
	seen := map[string]bool{}
	for p := range r.files {
		rp := r.redact(p)
		if !seen[rp] {
			seen[rp] = true
			rec.FilesChanged = append(rec.FilesChanged, rp)
		}
	}
	sort.Strings(rec.FilesChanged)
	r.emit(rec)
}
