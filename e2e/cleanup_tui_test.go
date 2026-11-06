//go:build linux

package e2e

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"yipyip/internal/provider/providertest"
)

// lockedBuffer is a bytes.Buffer safe for the pty drain goroutine and the
// test reading it.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func ptyIoctl(fd, req, arg uintptr) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg); e != 0 {
		return e
	}
	return nil
}

// openPty opens a new pty pair (stdlib only): the master and its slave,
// sized 120x40.
func openPty(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pty available: %v", err)
	}
	var unlock int32
	if err := ptyIoctl(master.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); err != nil {
		master.Close()
		t.Fatalf("TIOCSPTLCK: %v", err)
	}
	var n uint32
	if err := ptyIoctl(master.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); err != nil {
		master.Close()
		t.Fatalf("TIOCGPTN: %v", err)
	}
	slave, err = os.OpenFile("/dev/pts/"+strconv.FormatUint(uint64(n), 10), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		t.Fatalf("open slave: %v", err)
	}
	ws := [4]uint16{40, 120, 0, 0} // rows, cols, xpixel, ypixel
	_ = ptyIoctl(slave.Fd(), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws)))
	return master, slave
}

// tuiRun is the interactive yip started by startTUI.
type tuiRun struct {
	cmd    *exec.Cmd
	master *os.File      // pty master: keys are written here
	out    *lockedBuffer // everything yip wrote to its terminal
	done   chan error    // receives the Wait result once (put back after reading)
	dir    string
}

// startTUI runs the built binary interactively (no -p) with a pty as its
// controlling terminal, in a start dir whose fake provider first asks for
// the bash command of bashSleepsCall(arg) and then answers "done". It types
// "go" and Enter and waits until started.txt exists and the fake got its
// second request.
func startTUI(t *testing.T, arg string) *tuiRun {
	t.Helper()
	killSleepsOnCleanup(t, arg)
	srv := providertest.New(bashSleepsCall(arg), providertest.Response{Text: "done"})
	t.Cleanup(srv.Close)
	home, dir := headlessDirs(t, srv)
	master, slave := openPty(t)
	cmd := exec.Command(cliBinary(t))
	cmd.Dir = dir
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "TERM=xterm-256color"}, headlessEnv...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	r := &tuiRun{cmd: cmd, master: master, out: &lockedBuffer{}, done: make(chan error, 1), dir: dir}
	if err := cmd.Start(); err != nil {
		master.Close()
		slave.Close()
		t.Fatal(err)
	}
	slave.Close() // the child holds its own copy
	drained := make(chan struct{})
	go func() {
		_, _ = io.Copy(r.out, master) // ends with EIO once yip and its children closed the slave
		close(drained)
	}()
	go func() { r.done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case err := <-r.done:
			r.done <- err
		case <-time.After(5 * time.Second):
		}
		master.Close()
		select {
		case <-drained:
		case <-time.After(time.Second):
		}
	})
	// Type the prompt only once the UI has drawn its input box (the start
	// up terminal queries would swallow earlier keys).
	deadline := time.Now().Add(cliTimeout)
	// ("ype": the cursor styling splits the "T" of the placeholder off.)
	for !strings.Contains(r.out.String(), "ype a message") {
		if time.Now().After(deadline) {
			t.Fatalf("the UI never drew its input box\noutput %q", r.out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	r.write(t, "go\r")
	for {
		_, statErr := os.Stat(filepath.Join(dir, "started.txt"))
		if statErr == nil && len(srv.Requests()) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the turn never ran (started.txt: %v, requests: %d)\noutput %q",
				statErr, len(srv.Requests()), r.out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Let the UI apply the turn end before quitting.
	time.Sleep(300 * time.Millisecond)
	return r
}

func (r *tuiRun) write(t *testing.T, s string) {
	t.Helper()
	if _, err := io.WriteString(r.master, s); err != nil {
		t.Fatalf("write %q: %v", s, err)
	}
}

// exitWithin waits up to d for yip to exit and returns its Wait error.
func (r *tuiRun) exitWithin(t *testing.T, d time.Duration, what string) error {
	t.Helper()
	select {
	case err := <-r.done:
		r.done <- err
		return err
	case <-time.After(d):
		t.Fatalf("yip did not exit within %v of %s\noutput %q", d, what, r.out.String())
		return nil
	}
}

func checkExitZero(t *testing.T, err error, r *tuiRun) {
	t.Helper()
	if err != nil {
		var ee *exec.ExitError
		code := -1
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
		t.Fatalf("exit %v (code %d), want 0\noutput %q", err, code, r.out.String())
	}
}

func checkSessionSaved(t *testing.T, dir string) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, ".yipyip", "sessions", "*"))
	if len(files) == 0 {
		t.Fatal("no session file was saved")
	}
}

func TestCleanupTUIQuit(t *testing.T) {
	r := startTUI(t, "3051")
	r.write(t, "/quit\r")
	checkExitZero(t, r.exitWithin(t, 5*time.Second, "/quit"), r)
	checkSleepsGone(t, "3051")
}

func TestCleanupTUICtrlC(t *testing.T) {
	r := startTUI(t, "3052")
	r.write(t, "\x03\x03")
	r.exitWithin(t, 5*time.Second, "Ctrl+C twice")
	checkSleepsGone(t, "3052")
}

func testCleanupTUISignal(t *testing.T, arg string, sig syscall.Signal) {
	r := startTUI(t, arg)
	if err := r.cmd.Process.Signal(sig); err != nil {
		t.Fatal(err)
	}
	r.exitWithin(t, 2500*time.Millisecond, sig.String())
	checkSessionSaved(t, r.dir)
	checkSleepsGone(t, arg)
}

func TestCleanupTUISigterm(t *testing.T) {
	testCleanupTUISignal(t, "3053", syscall.SIGTERM)
}

func TestCleanupTUISighup(t *testing.T) {
	testCleanupTUISignal(t, "3054", syscall.SIGHUP)
}
