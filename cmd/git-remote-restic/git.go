package main

import (
	"bufio"
	"bytes"
	"fmt"
	urlparser "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/CGamesPlay/git-remote-restic/pkg/resticfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/pkg/errors"
)

var localGitPath string
var returnedCredentials string

const anonymous = "anonymous"

func init() {
	localGitPath = os.Getenv("GIT_DIR")
	if localGitPath == "" {
		localGitPath = git.GitDirName
	}
}

// FetchBatch is reponsible for fetching a batch of remote refs and storing
// them locally; implemented by "pushing" the refs from the restic repo into
// the local repo.
func FetchBatch(fetchSpecs [][]string) error {
	lock, err := sharedRepo.Lock(false)
	if err != nil {
		return err
	}
	defer func() {
		sharedRepo.Unlock(lock)
	}()
	repo, err := sharedRepo.Git(false)
	if err != nil {
		return err
	}
	remote, err := repo.CreateRemoteAnonymous(&config.RemoteConfig{
		Name: anonymous,
		URLs: []string{localGitPath},
	})
	if err != nil {
		return err
	}

	var refSpecs []config.RefSpec
	var deleteRefSpecs []config.RefSpec
	for i, fetch := range fetchSpecs {
		if len(fetch) != 2 {
			return errors.Errorf("Bad fetch request: %v", fetch)
		}
		refInBareRepo := fetch[1]

		// Push into a local ref with a temporary name, because the
		// git process that invoked us will get confused if we make a
		// ref with the same name.  Later, delete this temporary ref.
		localTempRef := fmt.Sprintf("%s-%d-%d",
			plumbing.ReferenceName(refInBareRepo).Short(), os.Getpid(), i)
		refSpec := fmt.Sprintf(
			"%s:refs/remotes/%s/%s", refInBareRepo, remoteName, localTempRef)

		refSpecs = append(refSpecs, config.RefSpec(refSpec))
		deleteRefSpecs = append(deleteRefSpecs, config.RefSpec(
			fmt.Sprintf(":refs/remotes/%s/%s", remoteName, localTempRef)))
	}

	err = remote.PushContext(globalCtx, &git.PushOptions{
		RemoteName: anonymous,
		RefSpecs:   refSpecs,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return err
	}

	err = remote.PushContext(globalCtx, &git.PushOptions{
		RemoteName: anonymous,
		RefSpecs:   deleteRefSpecs,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return err
	}

	return nil
}

// PushBatch is responsible for pushing a set of refs to the restic remote;
// implemented by "pulling" the refs from the local repository into the restic
// repo.
func PushBatch(refspecs []config.RefSpec) (map[string]error, error) {
	lock, err := sharedRepo.Lock(true)
	if err != nil {
		return nil, err
	}
	defer func() {
		sharedRepo.Unlock(lock)
	}()
	sharedRepo.fs.StartNewSnapshot()

	repo, err := sharedRepo.Git(true)
	if err != nil {
		return nil, errors.Wrap(err, "unable to open git remote")
	}
	remote, err := repo.CreateRemoteAnonymous(&config.RemoteConfig{
		Name: anonymous,
		URLs: []string{localGitPath},
	})
	if err != nil {
		return nil, err
	}

	results := make(map[string]error, len(refspecs))
	// Since we operate in reverse, we need to flip the refspecs around when we
	// fetch them from the local repository. This stores a list of the refs, in
	// reverse, which actually need to be fetched.
	fetchRefspecs := make([]config.RefSpec, 0, len(refspecs))
	for _, refspec := range refspecs {
		dst := refspec.Dst("")
		if refspec.IsDelete() {
			if refspec.IsWildcard() {
				results[dst.String()] = fmt.Errorf("wildcards (%#v) not supported for deletes", refspec)
				continue
			}
			err := repo.Storer.RemoveReference(dst)
			if err == git.NoErrAlreadyUpToDate {
				err = nil
			}
			results[dst.String()] = err
		} else {
			fetchRefspecs = append(fetchRefspecs, refspec.Reverse())
		}
	}

	err = remote.FetchContext(globalCtx, &git.FetchOptions{
		RemoteName: anonymous,
		RefSpecs:   refspecs,
	})
	if err == git.NoErrAlreadyUpToDate {
		err = nil
	}

	for _, refspec := range refspecs {
		if !refspec.IsDelete() {
			results[refspec.Dst("").String()] = err
		}
	}

	_, err = sharedRepo.fs.CommitSnapshot(localGitPath, []string{})
	if err != nil && err != resticfs.ErrNoChanges {
		return nil, err
	}

	return results, nil
}

func gitBin() string {
	gitExec := os.Getenv("GIT_EXEC_PATH")
	return filepath.Join(gitExec, "git")
}

func getGitCredential(urlStr string) (string, error) {
	url, err := urlparser.Parse(urlStr)
	if err != nil {
		Warnf("%s\n", urlStr)
		return "", err
	}
	input := fmt.Sprintf("protocol=%s\nhost=%s\npath=%s\nusername=%s\n\n", "restic", "none", url.Opaque, url.User.Username())
	cmd := exec.Command(gitBin(), "credential", "fill")
	cmd.Stdin = strings.NewReader(input)
	var out bytes.Buffer
	cmd.Stdout = &out
	err = cmd.Run()
	if err != nil {
		return "", err
	}
	returnedCredentials = string(out.Bytes())
	reader := bufio.NewReader(&out)
	for {
		prefix, err := reader.ReadString('=')
		if err != nil {
			return "", err
		}
		argument, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		if prefix == "password=" {
			return argument[:len(argument)-1], nil
		}
	}
}

func confirmGitCredential(url string, success bool) error {
	if returnedCredentials == "" {
		// Password didn't come from git credential
		return nil
	}
	var action = "reject"
	if success {
		action = "approve"
	}
	cmd := exec.Command(gitBin(), "credential", action)
	cmd.Stdin = strings.NewReader(returnedCredentials)
	var out bytes.Buffer
	cmd.Stdout = &out
	return cmd.Run()
}
