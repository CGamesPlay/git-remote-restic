package main

import (
	"fmt"
	"os"
)

// Warnf prints a message to standard error.
func Warnf(msg string, values ...interface{}) {
	fmt.Fprintf(os.Stderr, msg, values...)
}
