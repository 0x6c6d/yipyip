# v12 spec: no leftover processes after exit

## 1. Goal
Closing yip must leave no process behind that yip started. Today `bash` runs each command in its own
process group that is killed only on timeout/cancel, so anything the model backgrounds (`cmd &`, `nohup`,
dev servers, watchers, `setsid` daemons) from the main agent or a subagent keeps running after yip quits.
v12 tracks every such process and terminates them on every exit path.

## 2. New requirements
- R101: `tools.BashRunner` (the only process starter) registers every command it starts in a process-wide
  tracker (command pid = process-group id; works for the main agent and all subagents, which share it).
- R102: Reaper in `internal/tools` (stdlib `syscall` only, Linux): `KillAll(grace time.Duration)` sends SIGTERM
  to every tracked process group that still has live members and to every remaining descendant of the yip
  process, waits up to `grace` (poll ≤ 50 ms, returns early when none is left), then SIGKILLs what is left.
  It is idempotent, safe to call concurrently, and never signals yip itself or a pid that is not a tracked
  group or a descendant (no pid-reuse kills: a group is signalled only if `kill(-pgid, 0)` succeeds and it
  is still tracked).
- R103: Escapees: at startup yip sets `PR_SET_CHILD_SUBREAPER` (via `syscall.RawSyscall(SYS_PRCTL, …)`;
  failure is ignored), so processes that daemonize with `setsid`/double-fork are re-parented to yip.
  `KillAll` finds descendants by walking `/proc/<pid>/stat` ppid links from yip's own pid, repeating until a
  pass finds none new, so these processes are killed too.
- R104: Exit paths that call `KillAll(2s)` before the process ends: (a) every TUI quit (Ctrl+C/Ctrl+D quit,
  `/quit`, normal return of the Bubble Tea program, after the session save); (b) `yip -p` at the end of the
  run, also after `--timeout`, `--max-model-calls`, question exit and SIGINT/SIGTERM; (c) a crash exit
  (panic → exit 2, after the crash record is written); (d) SIGTERM and SIGHUP in the TUI (terminal closed,
  `kill <pid>`): the session is saved as on a normal quit, then `KillAll`, then exit.
- R105: The exit delay caused by cleanup is at most 2.5 s; with no leftover processes it is under 100 ms.
- R106: A running (not backgrounded) command that is cancelled by quit is killed as before (no regression of
  the cancel/timeout kill); a subagent running at quit time is cancelled first, then `KillAll` runs.
- R107: The agent system prompt states that processes still running when yip exits are terminated (so
  long-lived servers must not be expected to survive), one sentence.
- R108: `README.md` documents the cleanup (what is killed, when, the 2 s grace, Linux-only subreaper) and
  `/help` has one line about it.

## 3. Changed requirements
none

## 4. Removed requirements
none

## 5. Constraints & environment changes
none (stdlib only, Linux; no new packages, tools, secrets or live systems). The reaper lives in
`internal/tools/bash.go` or a new file of that package (`reaper.go`) and counts as security-critical like
`bash.go`: other code only calls `tools.KillAll`. No other package imports `os/exec` or `syscall.Kill`.

## 6. Acceptance criteria
- [ ] [R101,R102] `go test ./internal/tools -run Reaper`: `bash` command `sleep 300 &` returns at once; the
  sleep is alive; `KillAll(2s)` makes it gone (checked with `kill -0`/`/proc`), within the grace time.
- [ ] [R102] test: a child that traps SIGTERM (`trap '' TERM; sleep 300 &`) is gone after `KillAll(300ms)`
  (SIGKILL fallback); a second `KillAll` call is a no-op returning at once.
- [ ] [R102] test: an already exited group is not signalled (tracker entry dropped or `kill(-pgid,0)` fails);
  a bystander process of the test (own group, not started by BashRunner) survives `KillAll`.
- [ ] [R103] test: `setsid sleep 300 &` (new session) is gone after `KillAll`; `PR_SET_CHILD_SUBREAPER` is
  set in the test process (`/proc/self/status` or `prctl` get) after the init call.
- [ ] [R101,R106] test with the fake provider: main agent and a subagent each start a background `sleep`;
  after the session shuts down via the exit path, neither is alive.
- [ ] [R104a,R104d] TUI/e2e test: quit with Ctrl+C/`/quit` after a background `sleep 300 &` → no such
  process left; a SIGTERM to the built `yip` in a pty-less run saves the session and leaves none.
- [ ] [R104b] headless e2e: `yip -p` whose fake-provider turn runs `sleep 300 & echo ok` exits 0 and the
  sleep is gone when yip has exited; same for `--timeout` (exit 4) and `--max-model-calls` (exit 5).
- [ ] [R104c] crash e2e (`-X main.testPanic=…`): exit code 2, crash record present, background sleep gone.
- [ ] [R105] test: `KillAll` with nothing left returns in < 100 ms; with a TERM-ignoring child in < 2.5 s.
- [ ] [R107] `go test ./internal/agent -run Prompt`: the system prompt contains the sentence.
- [ ] [R108] `grep -ci 'background' README.md` ≥ 1 and `/help` text test finds the line.
- [ ] [R101-R108] `make check` still exits 0 (earlier versions keep working), and
  `grep -rn 'syscall.Kill' --include=*.go . | grep -v _test | grep -v internal/tools/` prints nothing.

## 7. Out of scope
Surviving-job features (detach, `nohup` allow-list, a jobs panel or `/jobs` command), a `bash` background
mode/tool, killing processes of other yip instances, non-Linux subreaper (other platforms: process groups only),
cgroups/namespaces/sandboxing, cleanup after SIGKILL of yip itself (not catchable).
