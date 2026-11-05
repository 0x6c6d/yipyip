package main

import (
	"fmt"
	"io"
)

// runHeadless is a stub; the headless runner replaces it.
func runHeadless(f flags, d deps, stdin io.Reader, stdout, stderr io.Writer, cs *crashState) int {
	fmt.Fprintln(stderr, "yip: headless mode not implemented")
	return 1
}
