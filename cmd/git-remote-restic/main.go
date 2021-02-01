package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/pkg/errors"
	"github.com/restic/restic/lib/repository"
)

var sharedRepo *Repository
var remoteName plumbing.ReferenceName
var reader *bufio.Reader
var printProgress = false
var verbosity = 1
var globalCtx = context.Background()

func cmdCapabilities() error {
	fmt.Printf("fetch\n")
	fmt.Printf("push\n")
	fmt.Printf("option\n")
	fmt.Printf("\n")
	return nil
}

func cmdList(forPush bool) error {
	repo, err := sharedRepo.Git(false)
	if err == git.ErrRepositoryNotExists {
		fmt.Print("\n")
		return nil
	}
	if err != nil {
		return err
	}
	refs, err := repo.References()
	if err != nil {
		return err
	}

	var symRefs []string
	hashesSeen := false
	for {
		ref, err := refs.Next()
		if errors.Cause(err) == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		value := ""
		switch ref.Type() {
		case plumbing.HashReference:
			value = ref.Hash().String()
			hashesSeen = true
		case plumbing.SymbolicReference:
			value = "@" + ref.Target().String()
		default:
			value = "?"
		}
		refStr := value + " " + ref.Name().String() + "\n"
		if ref.Type() == plumbing.SymbolicReference {
			// Don't list any symbolic references until we're sure
			// there's at least one object available.  Otherwise
			// cloning an empty repo will result in an error because
			// the HEAD symbolic ref points to a ref that doesn't
			// exist.
			symRefs = append(symRefs, refStr)
			continue
		}
		fmt.Print(refStr)
	}

	if hashesSeen && !forPush {
		for _, refStr := range symRefs {
			fmt.Print(refStr)
		}
	}
	fmt.Print("\n")
	return nil
}

func cmdOption(command string) error {
	switch {
	case command == "progress true":
		printProgress = true
		goto ok
	case command == "cloning true":
		// Nothing different here
		goto ok
	case strings.HasPrefix(command, "verbosity "):
		newV, err := strconv.Atoi(command[10:len(command)])
		if err != nil {
			fmt.Printf("error %v", err)
			return nil
		}
		verbosity = newV
		goto ok
	case false == true:
		// This tells go-vet that the panic below is "reachable".
	default:
		Warnf("unsupported option %#v\n", command)
		goto unsupported
	}
	panic("option parsing failed")
unsupported:
	fmt.Printf("unsupported\n")
	return nil
ok:
	fmt.Printf("ok\n")
	return nil
}

func cmdFetch(param string) error {
	fetchSpecs := make([][]string, 1)
	fetchSpecs[0] = strings.SplitN(param, " ", 2)
	if len(fetchSpecs[0]) != 2 {
		return fmt.Errorf("invalid fetch declaration %#v", param)
	}
loop:
	for {
		command, err := reader.ReadString('\n')
		if err != nil {
			return err
		}

		switch {
		case strings.HasPrefix(command, "fetch "):
			param = command[6 : len(command)-1]
			fetchSpecs = append(fetchSpecs, nil)
			fetchSpecs[len(fetchSpecs)-1] = strings.SplitN(param, " ", 2)
			if len(fetchSpecs[len(fetchSpecs)-1]) != 2 {
				return fmt.Errorf("invalid fetch declaration %#v", param)
			}
		case command == "\n":
			break loop
		default:
			return fmt.Errorf("unknown push command %q", command)
		}
	}

	if err := FetchBatch(fetchSpecs); err != nil {
		return err
	}
	fmt.Printf("\n")
	return nil
}

func cmdPush(param string) error {
	refspecs := make([]config.RefSpec, 1)
	refspecs[0] = config.RefSpec(param)
	if err := refspecs[0].Validate(); err != nil {
		return err
	}
loop:
	for {
		command, err := reader.ReadString('\n')
		if err != nil {
			return err
		}

		switch {
		case strings.HasPrefix(command, "push "):
			param = command[5 : len(command)-1]
			refspecs = append(refspecs, "")
			refspecs[len(refspecs)-1] = config.RefSpec(param)
			if err = refspecs[len(refspecs)-1].Validate(); err != nil {
				return err
			}
		case command == "\n":
			break loop
		default:
			return fmt.Errorf("unknown push command %q", command)
		}
	}

	results, err := PushBatch(refspecs)
	if err != nil {
		return err
	}
	for dst, err := range results {
		if err == nil {
			fmt.Printf("ok %s\n", dst)
		} else {
			fmt.Printf("error %s %#v\n", dst, err)
		}
	}
	fmt.Printf("\n")
	return nil
}

func findPassword(url string) (string, error) {
	password := os.Getenv("RESTIC_PASSWORD")
	if password != "" {
		return password, nil
	}

	pwFile := os.Getenv("RESTIC_PASSWORD_FILE")
	if pwFile != "" {
		data, err := ioutil.ReadFile(pwFile)
		password = strings.TrimSpace(string(data))
		if err != nil {
			return "", err
		}
		return password, nil
	}

	return getGitCredential(url)
}

// Main entry point.
func Main() (err error) {
	reader = bufio.NewReader(os.Stdin)

	if len(os.Args) > 1 && os.Args[1] == "--version" {
		PrintVersion()
		return nil
	} else if len(os.Args) < 3 {
		return fmt.Errorf("Usage: %s remote-name url", os.Args[0])
	}

	remoteName = plumbing.ReferenceName(os.Args[1])
	url := os.Args[2]

	password, err := findPassword(url)
	if err != nil {
		return err
	}

	sharedRepo, err = NewRepository(context.Background(), url, password)
	if err != nil {
		if err == repository.ErrNoKeyFound {
			confirmGitCredential(url, false)
		}
		return err
	}
	confirmGitCredential(url, true)

	for {
		// Note that command will include the trailing newline.
		command, err := reader.ReadString('\n')
		if err != nil {
			return err
		}

		switch {
		case command == "capabilities\n":
			if err = cmdCapabilities(); err != nil {
				return err
			}
		case command == "list\n" || command == "list for-push\n":
			if err = cmdList(command == "list for-push\n"); err != nil {
				return err
			}
		case strings.HasPrefix(command, "option "):
			if err = cmdOption(command[7 : len(command)-1]); err != nil {
				return err
			}
		case strings.HasPrefix(command, "fetch "):
			if err = cmdFetch(command[6 : len(command)-1]); err != nil {
				return err
			}
		case strings.HasPrefix(command, "push "):
			if err = cmdPush(command[5 : len(command)-1]); err != nil {
				return err
			}
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
