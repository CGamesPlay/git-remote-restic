package main

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// KnownMark stores information about a single mark and how it is represented
// in git/restic.
type KnownMark struct {
	Name     string
	Sha1     [sha1.Size]byte
	ResticID string
}

var allMarks = map[string]KnownMark{}

// CommitMetadata stores all of the metadata about a commit.
type CommitMetadata struct {
	Author    string
	Committer string
	Message   string
	From      string
	Merge     []string
}

// FastImport implements the git fast-import protocol to store the repository
// in restic.
func FastImport() error {
	lock, err := lockRepository(globalCtx, resticRepo, false)
	if err != nil {
		return err
	}
	defer unlockRepo(lock)
	for {
		command, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		switch {
		case command[0] == '#':
			continue
		case strings.HasPrefix(command, "feature "):
			if command == "feature done\n" {
				continue
			}
			return fmt.Errorf("Unsupported feature %q", command)
		case command == "blob\n":
			if err := importBlob(globalCtx); err != nil {
				return err
			}
		case strings.HasPrefix(command, "reset "):
			if err := importCommit(globalCtx, command[6:len(command)-1]); err != nil {
				return err
			}
		default:
			return fmt.Errorf("Received unknown command %q", command)
		}

	}
}

func importBlob(ctx context.Context) error {
	var buffer []byte
	mark := KnownMark{}
loop:
	for {
		command, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		switch {
		case strings.HasPrefix(command, "mark "):
			mark.Name = command[5 : len(command)-1]
		case strings.HasPrefix(command, "data "):
			if buffer, err = readData(command); err != nil {
				return err
			}
			break loop
		default:
			return fmt.Errorf("Received unknown command %q", command)
		}
	}
	if mark.Name == "" {
		// XXX - I'm pretty sure the answer here is to store the sha1 as the
		// mark name, but this needs to be confirmed.
		return fmt.Errorf("implicit mark not supported")
	}
	mark.Sha1 = sha1.Sum(buffer)
	allMarks[mark.Name] = mark
	return nil
}

func readData(command string) ([]byte, error) {
	bytes, err := strconv.Atoi(command[5 : len(command)-1])
	if err != nil {
		return []byte{}, err
	}
	buffer := make([]byte, bytes)
	if _, err := io.ReadFull(reader, buffer); err != nil {
		return []byte{}, err
	}
	check, err := reader.Peek(1)
	if err != nil {
		return []byte{}, err
	}
	if check[0] == '\n' {
		if _, err := reader.ReadByte(); err != nil {
			return []byte{}, err
		}
	}
	return buffer, nil
}

func importCommit(ctx context.Context, refName string) error {
	commit := CommitMetadata{}
	mark := KnownMark{}
	for {
		command, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		switch {
		case strings.HasPrefix(command, "commit "):
			refName = command[7 : len(command)-1]
		case strings.HasPrefix(command, "mark "):
			mark.Name = command[5 : len(command)-1]
		case strings.HasPrefix(command, "author "):
			commit.Author = command[7 : len(command)-1]
		case strings.HasPrefix(command, "committer "):
			commit.Committer = command[7 : len(command)-1]
		case strings.HasPrefix(command, "data "):
			buffer, err := readData(command)
			if err != nil {
				return err
			}
			commit.Message = string(buffer)
		default:
			return fmt.Errorf("Received unknown command %q", command)
		}
	}
}
