package main

import (
	"context"
	"fmt"
	"os"

	"github.com/CGamesPlay/git-remote-restic/pkg/filesystem"
	"github.com/go-git/go-billy/v5/helper/polyfill"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	gitfs "github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/pkg/errors"
	"github.com/restic/restic/lib/restic"
)

var remoteGitRepo *git.Repository
var localGitPath string

const anonymous = "anonymous"

func init() {
	fs := osfs.New("../git.bare")
	s := gitfs.NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), gitfs.Options{KeepDescriptors: true})
	var err error
	remoteGitRepo, err = git.Open(s, fs)
	if err != nil {
		panic(err)
	}

	localGitPath = os.Getenv("GIT_DIR")
	if localGitPath == "" {
		localGitPath = git.GitDirName
	}
}

func initRemoteGitRepo() error {
	if remoteGitRepo != nil {
		return nil
	}
	id, err := restic.FindLatestSnapshot(context.Background(), resticRepo, nil, nil, nil)
	if err != nil {
		return err
	}
	snapshot, err := restic.LoadSnapshot(context.Background(), resticRepo, id)
	if err != nil {
		return err
	}
	fs, err := filesystem.NewResticTreeFs(context.Background(), resticRepo, snapshot.Tree)
	if err != nil {
		return err
	}
	pf := polyfill.New(fs)
	s := gitfs.NewStorageWithOptions(pf, cache.NewObjectLRUDefault(), gitfs.Options{KeepDescriptors: true})
	remoteGitRepo, err = git.Open(s, pf)
	return err
}

// FetchBatch is reponsible for fetching a batch of remote refs and storing
// them locally.
func FetchBatch(fetchSpecs [][]string) error {
	// Go-git's high-level API doesn't support the case where the "remote"
	// repository is backed by a custom VFS, so we do operations in reverse:
	// when pushing to restic, we actually pull from the local repository; and
	// vice versa.

	remote, err := remoteGitRepo.CreateRemoteAnonymous(&config.RemoteConfig{
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

// PushBatch is responsible for pushing a set of refs to the restic remote.
func PushBatch(refspecs []config.RefSpec) (map[string]error, error) {
	remote, err := remoteGitRepo.CreateRemoteAnonymous(&config.RemoteConfig{
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
		if refspec.IsDelete() {
			err := remoteGitRepo.Storer.RemoveReference(refspec.Dst(""))
			if err == git.NoErrAlreadyUpToDate {
				err = nil
			}
			results[refspec.Dst("").String()] = err
		} else {
			fetchRefspecs = append(fetchRefspecs, refspec.Reverse())
		}
	}

	err = remote.FetchContext(globalCtx, &git.FetchOptions{
		RemoteName: anonymous,
		RefSpecs:   refspecs,
		Tags:       git.NoTags, // TODO - implement
		Force:      false,      // TODO - implement
	})
	if err == git.NoErrAlreadyUpToDate {
		err = nil
	}

	for _, refspec := range refspecs {
		if !refspec.IsDelete() {
			results[refspec.Dst("").String()] = err
		}
	}
	return results, nil
}
