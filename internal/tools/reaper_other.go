//go:build !linux

package tools

import (
	"syscall"
	"time"
)

// KillAll terminates every live process group started by BashRunner.Run:
// SIGTERM first, then SIGKILL for the groups still live after grace. Without
// /proc a group counts as live while kill(-pgid, 0) succeeds. Concurrent
// calls run one after the other.
func KillAll(grace time.Duration) {
	killAll(grace, groupsLiveKill)
}

func groupsLiveKill(pgids []int) []bool {
	out := make([]bool, len(pgids))
	for i, pgid := range pgids {
		out[i] = pgid > 1 && syscall.Kill(-pgid, 0) == nil
	}
	return out
}
