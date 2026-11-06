package tools

import (
	"os"
	"sync"
	"syscall"
	"time"
)

// killPollInterval is the polling period of KillAll while it waits for
// signalled groups to exit.
const killPollInterval = 25 * time.Millisecond

// tracker holds the process groups started by BashRunner.Run. A group stays
// tracked after its leader exited (inactive) because background members may
// still live in it; KillAll drops it once the group is no longer live.
var tracker = struct {
	mu     sync.Mutex
	groups map[int]bool // pgid -> active (command still inside Run)
}{groups: map[int]bool{}}

// killMu serialises KillAll callers.
var killMu sync.Mutex

// track records pgid as an active tracked group.
func track(pgid int) {
	tracker.mu.Lock()
	tracker.groups[pgid] = true
	tracker.mu.Unlock()
}

// untrackActive marks pgid inactive; the entry stays until KillAll finds the
// group dead.
func untrackActive(pgid int) {
	tracker.mu.Lock()
	if _, ok := tracker.groups[pgid]; ok {
		tracker.groups[pgid] = false
	}
	tracker.mu.Unlock()
}

// trackedPGIDs returns a snapshot of the tracked pgids.
func trackedPGIDs() []int {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	out := make([]int, 0, len(tracker.groups))
	for pgid := range tracker.groups {
		out = append(out, pgid)
	}
	return out
}

// isTracked reports whether pgid is still tracked.
func isTracked(pgid int) bool {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	_, ok := tracker.groups[pgid]
	return ok
}

// dropIfInactive removes pgid unless its command is still inside Run.
func dropIfInactive(pgid int) {
	tracker.mu.Lock()
	if active, ok := tracker.groups[pgid]; ok && !active {
		delete(tracker.groups, pgid)
	}
	tracker.mu.Unlock()
}

// protectedPGID reports whether pgid must never be signalled.
func protectedPGID(pgid int) bool {
	return pgid <= 1 || pgid == os.Getpid() || pgid == syscall.Getpgrp()
}

// killSet is the set of groups KillAll signals. It is a struct so a later
// step (single descendant pids) can be added beside the groups.
type killSet struct {
	groups []int
}

func (k killSet) empty() bool { return len(k.groups) == 0 }

// collectKillSet returns the tracked groups that are live according to
// groupsLive. Groups that are not live are dropped from the tracker (unless
// still active) and never signalled.
func collectKillSet(groupsLive func([]int) []bool) killSet {
	var cand []int
	for _, pgid := range trackedPGIDs() {
		if protectedPGID(pgid) {
			dropIfInactive(pgid)
			continue
		}
		cand = append(cand, pgid)
	}
	var ks killSet
	if len(cand) == 0 {
		return ks
	}
	live := groupsLive(cand)
	for i, pgid := range cand {
		if live[i] {
			ks.groups = append(ks.groups, pgid)
		} else {
			dropIfInactive(pgid)
		}
	}
	return ks
}

// liveSubset re-checks ks and returns the groups still live.
func liveSubset(ks killSet, groupsLive func([]int) []bool) killSet {
	if ks.empty() {
		return ks
	}
	live := groupsLive(ks.groups)
	var out killSet
	for i, pgid := range ks.groups {
		if live[i] {
			out.groups = append(out.groups, pgid)
		}
	}
	return out
}

// signalSet sends sig to every group of ks.
func signalSet(ks killSet, sig syscall.Signal) {
	for _, pgid := range ks.groups {
		if protectedPGID(pgid) || !isTracked(pgid) {
			continue
		}
		_ = syscall.Kill(-pgid, sig)
	}
}

// killAll implements KillAll with an OS-specific liveness check.
func killAll(grace time.Duration, groupsLive func([]int) []bool) {
	killMu.Lock()
	defer killMu.Unlock()

	ks := collectKillSet(groupsLive)
	if ks.empty() {
		return
	}
	signalSet(ks, syscall.SIGTERM)

	deadline := time.Now().Add(grace)
	for {
		ks = liveSubset(ks, groupsLive)
		if ks.empty() || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(min(killPollInterval, time.Until(deadline)))
	}
	if !ks.empty() {
		signalSet(ks, syscall.SIGKILL)
	}
	// Drop the entries whose groups are dead now.
	_ = collectKillSet(groupsLive)
}
