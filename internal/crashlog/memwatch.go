package crashlog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Memory watchdog. An OOM kill is a SIGKILL and leaves no crash record, so
// Watch samples memory while the process runs and appends a "memory" record
// when the Go heap crosses a limit or the machine runs low on memory. The
// record names the largest processes, so a runaway child shows up too.
const (
	WatchInterval = 2 * time.Second
	HeapLimit     = 1 << 30 // first heap record; each next one needs double
	LowAvailable  = 2 << 30 // MemAvailable (RAM only, swap excluded) below this counts as low
	maxProcs      = 6       // processes listed in a record
	heapProfRel   = ".yipyip/heap.pprof"
)

// Watch samples memory every interval until ctx is done. A nil Logger does
// nothing. Records are rate limited: the heap one repeats only after the heap
// doubled, the low-memory one at most once a minute.
func (l *Logger) Watch(ctx context.Context, interval time.Duration) {
	if l == nil {
		return
	}
	defer func() { _ = recover() }()
	t := time.NewTicker(interval)
	defer t.Stop()
	heapNext := uint64(HeapLimit)
	var lastLow time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		avail, ok := availableBytes()
		switch {
		case ms.HeapAlloc >= heapNext:
			heapNext = ms.HeapAlloc * 2
			l.memoryRecord("heap above limit", &ms, true)
		case ok && avail < LowAvailable && time.Since(lastLow) > time.Minute:
			lastLow = time.Now()
			l.memoryRecord("system memory low", &ms, false)
		}
	}
}

// memoryRecord appends one "memory" record; withProfile also writes the heap
// profile to .yipyip/heap.pprof (replaced each time). It never panics.
func (l *Logger) memoryRecord(reason string, ms *runtime.MemStats, withProfile bool) {
	defer func() { _ = recover() }()
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s memory ===\n", recordHead, time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "version: %s\n", oneLine(l.redact(l.o.Version)))
	if l.o.SessionID != nil {
		if id := l.o.SessionID(); id != "" {
			fmt.Fprintf(&b, "session: %s\n", oneLine(l.redact(id)))
		}
	}
	fmt.Fprintf(&b, "reason: %s\n", reason)
	fmt.Fprintf(&b, "heap_alloc_mib: %d\nheap_inuse_mib: %d\nsys_mib: %d\ngoroutines: %d\n",
		ms.HeapAlloc>>20, ms.HeapInuse>>20, ms.Sys>>20, runtime.NumGoroutine())
	if avail, ok := availableBytes(); ok {
		fmt.Fprintf(&b, "available_mib: %d\n", avail>>20)
	}
	b.WriteString("processes (pid rss_mib name):\n")
	for _, p := range topProcesses(maxProcs) {
		fmt.Fprintf(&b, "  %d %d %s\n", p.pid, p.rssKiB>>10, oneLine(l.redact(p.name)))
	}
	if withProfile {
		if p, err := l.writeHeapProfile(); err == nil {
			fmt.Fprintf(&b, "heap_profile: %s\n", p)
		}
	}
	b.WriteString(endLine)
	_, _ = l.write([]byte(noHeaders(b.String())))
}

// writeHeapProfile replaces the heap profile file and returns its path.
func (l *Logger) writeHeapProfile() (string, error) {
	path, err := l.Resolve(heapProfRel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	werr := pprof.Lookup("heap").WriteTo(f, 0)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return path, werr
}

// Resolve maps a start-dir-relative path through Options.Resolve.
func (l *Logger) Resolve(rel string) (string, error) {
	if l.o.Resolve == nil {
		return "", fmt.Errorf("crashlog: no Resolve configured")
	}
	return l.o.Resolve(rel)
}

// availableBytes returns MemAvailable from /proc/meminfo. Swap is left out on
// purpose: once RAM is gone the machine thrashes and a record may never be written.
func availableBytes() (uint64, bool) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	var avail uint64
	var have bool
	for _, ln := range strings.Split(string(data), "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 {
			continue
		}
		v, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			continue
		}
		switch f[0] {
		case "MemAvailable:":
			avail, have = v<<10, true
		}
	}
	return avail, have
}

type procInfo struct {
	pid    int
	rssKiB uint64
	name   string
}

// topProcesses returns the n processes with the largest resident set, read
// from /proc; it returns nil where /proc is not available.
func topProcesses(n int) []procInfo {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []procInfo
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + e.Name() + "/status")
		if err != nil {
			continue
		}
		p := procInfo{pid: pid}
		for _, ln := range strings.Split(string(data), "\n") {
			switch {
			case strings.HasPrefix(ln, "Name:"):
				p.name = strings.TrimSpace(ln[len("Name:"):])
			case strings.HasPrefix(ln, "VmRSS:"):
				if f := strings.Fields(ln); len(f) >= 2 {
					p.rssKiB, _ = strconv.ParseUint(f[1], 10, 64)
				}
			}
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rssKiB > out[j].rssKiB })
	if len(out) > n {
		out = out[:n]
	}
	return out
}
