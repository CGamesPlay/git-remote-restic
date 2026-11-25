package main

import (
	"fmt"
	"os"
)

// Warnf must write to stderr instead of stdout, since the latter is used by the git remote-helper protocol.
func Warnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
}
