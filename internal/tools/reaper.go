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

// killReapWait bounds the wait after SIGKILL for killed processes to turn
// into zombies that can be reaped.
const killReapWait = 200 * time.Millisecond

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

// isActive reports whether pid is the leader of a command still inside
// BashRunner.Run (its pid is its tracked pgid).
func isActive(pid int) bool {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.groups[pid]
}

// procOps are the OS-specific steps of killAll. descendants and reap may be
// nil (no /proc): then only tracked groups are handled.
type procOps struct {
	// groupsLive reports for each pgid whether the group is live.
	groupsLive func([]int) []bool
	// descendants returns the non-zombie single pids of the kill set seen in
	// one /proc pass, given the tracked groups of this call.
	descendants func(groups map[int]bool) []int
	// reap reaps the zombie children of yip that belong to the kill set of
	// this call, given its tracked groups.
	reap func(groups map[int]bool)
}

// killSet is the set of groups and single pids KillAll signals.
type killSet struct {
	groups []int
	pids   []int
}

func (k killSet) empty() bool { return len(k.groups) == 0 && len(k.pids) == 0 }

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

// liveSubset re-checks the groups of ks and returns the ones still live.
func liveSubset(ks killSet, groupsLive func([]int) []bool) killSet {
	var out killSet
	if len(ks.groups) == 0 {
		return out
	}
	live := groupsLive(ks.groups)
	for i, pgid := range ks.groups {
		if live[i] {
			out.groups = append(out.groups, pgid)
		}
	}
	return out
}

// signalSet sends sig to every group and single pid of ks.
func signalSet(ks killSet, sig syscall.Signal) {
	for _, pgid := range ks.groups {
		if protectedPGID(pgid) || !isTracked(pgid) {
			continue
		}
		_ = syscall.Kill(-pgid, sig)
	}
	self := os.Getpid()
	for _, pid := range ks.pids {
		if pid <= 1 || pid == self {
			continue
		}
		_ = syscall.Kill(pid, sig)
	}
}

// addDescendants fills ks.pids from a fresh /proc pass (if available).
func addDescendants(ks *killSet, ops procOps, groups map[int]bool) {
	ks.pids = nil
	if ops.descendants != nil {
		ks.pids = ops.descendants(groups)
	}
}

// killAll implements KillAll with OS-specific process steps.
func killAll(grace time.Duration, ops procOps) {
	killMu.Lock()
	defer killMu.Unlock()

	ks := collectKillSet(ops.groupsLive)
	// groups: the tracked groups of this call (criterion (a) of the kill set).
	groups := map[int]bool{}
	for _, pgid := range ks.groups {
		groups[pgid] = true
	}
	addDescendants(&ks, ops, groups)
	reap := func() {
		if ops.reap != nil {
			ops.reap(groups)
		}
	}
	if ks.empty() {
		reap()
		return
	}
	signalSet(ks, syscall.SIGTERM)
	termed := map[int]bool{}
	for _, pid := range ks.pids {
		termed[pid] = true
	}

	deadline := time.Now().Add(grace)
	for {
		reap()
		ks = liveSubset(ks, ops.groupsLive)
		addDescendants(&ks, ops, groups)
		// New escapees found during the grace get SIGTERM too.
		var fresh killSet
		for _, pid := range ks.pids {
			if !termed[pid] {
				termed[pid] = true
				fresh.pids = append(fresh.pids, pid)
			}
		}
		signalSet(fresh, syscall.SIGTERM)
		if ks.empty() || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(min(killPollInterval, time.Until(deadline)))
	}
	if !ks.empty() {
		signalSet(ks, syscall.SIGKILL)
		// Wait briefly for the killed processes to die so their zombies
		// can be reaped.
		end := time.Now().Add(killReapWait)
		for {
			reap()
			ks = liveSubset(ks, ops.groupsLive)
			addDescendants(&ks, ops, groups)
			if ks.empty() || !time.Now().Before(end) {
				break
			}
			signalSet(ks, syscall.SIGKILL)
			time.Sleep(10 * time.Millisecond)
		}
	}
	reap()
	// Drop the entries whose groups are dead now.
	_ = collectKillSet(ops.groupsLive)
}
