package main

import (
	"fmt"
	"os"
)

// Touch ensures that the named file exists.
func Touch(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
	if os.IsExist(err) {
		return nil
	} else if err != nil {
		return err
	}

	return file.Close()
}

// Warnf prints a message to standard error.
func Warnf(msg string, values ...interface{}) {
	fmt.Fprintf(os.Stderr, msg, values...)
}
