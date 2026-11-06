//go:build linux

package tools

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// KillAll terminates every live process group started by BashRunner.Run and
// every descendant of yip that escaped them: SIGTERM first, then SIGKILL for
// what is still live after grace. A group is live only while it is tracked,
// kill(-pgid, 0) succeeds and /proc shows at least one non-zombie member. A
// descendant of yip (ppid walk over /proc) is signalled when its pgrp is a
// tracked group of this call, its parent is in the kill set, or its session
// differs from yip's. Zombie children of yip in the kill set are reaped with
// wait4(pid, WNOHANG), never wait4(-1). Concurrent calls run one after the
// other.
func KillAll(grace time.Duration) {
	killAll(grace, procOps{
		groupsLive:  groupsLiveProc,
		descendants: descendantsProc,
		reap:        reapProc,
	})
}

// InitSubreaper marks yip as child subreaper, so orphaned descendants are
// re-parented to yip instead of init and KillAll can find them. Errors are
// ignored; calling it again is harmless.
func InitSubreaper() {
	_, _, _ = syscall.RawSyscall(syscall.SYS_PRCTL, 36 /* PR_SET_CHILD_SUBREAPER */, 1, 0)
}

// procInfo holds the fields of /proc/<pid>/stat KillAll needs.
type procInfo struct {
	pid, ppid, pgrp, session int
	zombie                   bool
}

// readProcStat parses fields after the last ')' of a /proc/<pid>/stat line:
// state=3, ppid=4, pgrp=5, session=6.
func readProcStat(s string) (state string, ppid, pgrp, session int, ok bool) {
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return "", 0, 0, 0, false
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 4 {
		return "", 0, 0, 0, false
	}
	var nums [3]int
	for j := range nums {
		n, err := strconv.Atoi(f[j+1])
		if err != nil {
			return "", 0, 0, 0, false
		}
		nums[j] = n
	}
	return f[0], nums[0], nums[1], nums[2], true
}

// procPass reads every /proc/<pid>/stat once.
func procPass() []procInfo {
	d, err := os.Open("/proc")
	if err != nil {
		return nil
	}
	names, _ := d.Readdirnames(-1)
	_ = d.Close()
	out := make([]procInfo, 0, len(names))
	for _, name := range names {
		pid, err := strconv.Atoi(name)
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + name + "/stat")
		if err != nil {
			continue
		}
		state, ppid, pgrp, sess, ok := readProcStat(string(b))
		if !ok {
			continue
		}
		out = append(out, procInfo{pid: pid, ppid: ppid, pgrp: pgrp, session: sess, zombie: state == "Z"})
	}
	return out
}

// selfSession returns yip's session id (field 6 of /proc/self/stat), or -1.
func selfSession() int {
	b, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return -1
	}
	_, _, _, sess, ok := readProcStat(string(b))
	if !ok {
		return -1
	}
	return sess
}

// killSetMembers walks the descendants of yip in one /proc pass and returns
// those in the kill set (both live processes and zombies).
func killSetMembers(groups map[int]bool) []procInfo {
	self := os.Getpid()
	mySess := selfSession()
	children := map[int][]procInfo{}
	for _, p := range procPass() {
		children[p.ppid] = append(children[p.ppid], p)
	}
	inKill := map[int]bool{}
	visited := map[int]bool{self: true}
	var out []procInfo
	queue := []int{self}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, c := range children[parent] {
			if visited[c.pid] {
				continue
			}
			visited[c.pid] = true
			queue = append(queue, c.pid)
			if c.pid <= 1 || c.pid == self {
				continue
			}
			if groups[c.pgrp] || inKill[parent] || (mySess > 0 && c.session != mySess) {
				inKill[c.pid] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// descendantsProc returns the non-zombie kill-set descendants of one pass.
func descendantsProc(groups map[int]bool) []int {
	var out []int
	for _, p := range killSetMembers(groups) {
		if !p.zombie {
			out = append(out, p.pid)
		}
	}
	return out
}

// reapProc reaps zombie kill-set members whose parent is yip and that are not
// an active BashRunner command, one pid at a time.
func reapProc(groups map[int]bool) {
	self := os.Getpid()
	for _, p := range killSetMembers(groups) {
		if !p.zombie || p.ppid != self || isActive(p.pid) {
			continue
		}
		var ws syscall.WaitStatus
		_, _ = syscall.Wait4(p.pid, &ws, syscall.WNOHANG, nil)
	}
}

// groupsLiveProc reports for each pgid whether the group is live, using one
// scan of /proc.
func groupsLiveProc(pgids []int) []bool {
	out := make([]bool, len(pgids))
	anyLive := false
	for i, pgid := range pgids {
		out[i] = pgid > 1 && syscall.Kill(-pgid, 0) == nil
		anyLive = anyLive || out[i]
	}
	if !anyLive {
		return out
	}
	members := livePgrps()
	for i, pgid := range pgids {
		out[i] = out[i] && members[pgid]
	}
	return out
}

// livePgrps scans /proc/<pid>/stat and returns the process groups that have
// at least one non-zombie member.
func livePgrps() map[int]bool {
	res := map[int]bool{}
	d, err := os.Open("/proc")
	if err != nil {
		return res
	}
	names, _ := d.Readdirnames(-1)
	_ = d.Close()
	for _, name := range names {
		if _, err := strconv.Atoi(name); err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + name + "/stat")
		if err != nil {
			continue
		}
		state, pgrp, ok := parseStat(string(b))
		if !ok || state == "Z" {
			continue
		}
		res[pgrp] = true
	}
	return res
}

// parseStat extracts state (field 3) and pgrp (field 5) from a
// /proc/<pid>/stat line, parsing after the last ')' since comm may contain
// spaces and parentheses.
func parseStat(s string) (state string, pgrp int, ok bool) {
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return "", 0, false
	}
	f := strings.Fields(s[i+1:])
	// f[0]=state, f[1]=ppid, f[2]=pgrp
	if len(f) < 3 {
		return "", 0, false
	}
	pg, err := strconv.Atoi(f[2])
	if err != nil {
		return "", 0, false
	}
	return f[0], pg, true
}
