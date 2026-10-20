package provider

import (
	"bufio"
	"io"
	"strings"
)

// maxSSELine is the longest SSE line the parser accepts.
const maxSSELine = 1 << 20

// readSSE reads a server-sent-events stream and calls fn with the payload of
// every "data:" line (one optional space after the colon is dropped). Other
// fields, comments and blank lines are ignored. It stops when fn returns false,
// at EOF (nil error), or on a read error (including a line over 1 MiB).
func readSSE(r io.Reader, fn func(data string) bool) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxSSELine+2)
	for sc.Scan() {
		line := strings.TrimSuffix(sc.Text(), "\r")
		rest, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		rest = strings.TrimPrefix(rest, " ")
		if !fn(rest) {
			return nil
		}
	}
	return sc.Err()
}
