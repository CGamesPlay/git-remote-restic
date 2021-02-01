package main

import "fmt"

// Version is the version of the program.
var Version = "(unversioned)"

// ResticVersion is the version of restic this program is linked against.
var ResticVersion = "(unversioned)"

// PrintVersion prints a human-readable version string to stdout.
func PrintVersion() {
	fmt.Printf("git-remote-restic version %s, using restic version %s\n", Version, ResticVersion)
}
