// +build ignore

package main

import (
	"fmt"
	"os"
)

func init() {
	fmt.Fprintf(os.Stderr, "Press a key to start. PID %d\n", os.Getpid())
	f, err := os.Open("/dev/tty")
	if err != nil {
		panic(err)
	}
	b := make([]byte, 1)
	if _, err = f.Read(b); err != nil {
		panic(err)
	}
}
