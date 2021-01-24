package main

import (
	"bufio"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"

	"github.com/restic/restic/lib/repository"
)

var refspec string
var gitmarks string
var repo *repository.Repository
var reader *bufio.Reader

func cmdCapabilities() error {
	fmt.Printf("import\n")
	fmt.Printf("export\n")
	fmt.Printf("refspec %s\n", refspec)
	fmt.Printf("*import-marks %s\n", gitmarks)
	fmt.Printf("*export-marks %s\n", gitmarks)
	fmt.Printf("signed-tags\n")
	fmt.Printf("\n")
	return nil
}

func cmdList() error {
	fmt.Printf("\n")
	return nil
}

// Apparently sometimes the git marks file can become corrupted on a crash.
// This will restore it to the original value in case of a failure.
func preserveMarks() (func(err error), error) {
	originalGitmarks, err := ioutil.ReadFile(gitmarks)
	if err != nil {
		return nil, err
	}

	return func(err error) {
		if err != nil {
			ioutil.WriteFile(gitmarks, originalGitmarks, 0666)
		}
	}, nil
}

// Main entry point.
func Main() (err error) {
	ctx := context.Background()

	if len(os.Args) < 3 {
		return fmt.Errorf("Usage: %v remote-name url", os.Args[0])
	}

	remoteName := os.Args[1]
	url := os.Args[2]

	if repo, err = openBackend(url); err != nil {
		return err
	}
	refspec = fmt.Sprintf("refs/heads/*:refs/restic/%s/*", remoteName)

	localdir := path.Join(os.Getenv("GIT_DIR"), "restic", remoteName)
	if err := os.MkdirAll(localdir, 0755); err != nil {
		return err
	}

	gitmarks = path.Join(localdir, "gitmarks")
	if err := Touch(gitmarks); err != nil {
		return err
	}
	restoreMarks, err := preserveMarks()
	if err != nil {
		return err
	}
	defer restoreMarks(err)

	reader = bufio.NewReader(os.Stdin)
	for {
		// Note that command will include the trailing newline.
		command, err := reader.ReadString('\n')
		if err != nil {
			return err
		}

		switch {
		case command == "capabilities\n":
			err = cmdCapabilities()
			if err != nil {
				return err
			}
		case command == "list\n":
			err = cmdList()
			if err != nil {
				return err
			}
		case command == "export\n":
			return FastImport(ctx)
		case command == "\n":
			return nil
		default:
			return fmt.Errorf("Received unknown command %q", command)
		}
	}
}

func main() {
	if err := Main(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
