//go:build linux

package tools

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// KillAll terminates every live process group started by BashRunner.Run:
// SIGTERM first, then SIGKILL for the groups still live after grace. A group
// is live only while it is tracked, kill(-pgid, 0) succeeds and /proc shows at
// least one non-zombie member. Concurrent calls run one after the other.
func KillAll(grace time.Duration) {
	killAll(grace, groupsLiveProc)
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
