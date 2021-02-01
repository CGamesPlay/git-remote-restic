package main

import (
	"fmt"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/helper/chroot"
	billyutil "github.com/go-git/go-billy/v5/util"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	gitfs "github.com/go-git/go-git/v5/storage/filesystem"
)

func testReadGitCommit(fs billy.Filesystem) error {
	s := gitfs.NewStorageWithOptions(fs, cache.NewObjectLRUDefault(), gitfs.Options{KeepDescriptors: true})
	repo, err := git.Open(s, fs)
	if err != nil {
		return err
	}
	ref, err := repo.Head()
	if err != nil {
		return err
	}
	commit, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return err
	}
	fmt.Println(commit)
	tree, err := commit.Tree()
	if err != nil {
		return err
	}
	tree.Files().ForEach(func(f *object.File) error {
		fmt.Printf("100644 blob %s    %s\n", f.Hash, f.Name)
		return nil
	})

	return nil
}

func testMakeGitCommit(fs billy.Filesystem) error {
	gitDir := chroot.New(fs, ".git")
	s := gitfs.NewStorageWithOptions(gitDir, cache.NewObjectLRUDefault(), gitfs.Options{KeepDescriptors: true})
	repo, err := git.Init(s, fs)
	if err != nil {
		return err
	}
	w, err := repo.Worktree()
	if err != nil {
		return err
	}
	err = billyutil.WriteFile(fs, "README.md", []byte("Hello, world!"), 0)
	if err != nil {
		return err
	}
	_, err = w.Add("README.md")
	if err != nil {
		return err
	}
	status, err := w.Status()
	if err != nil {
		return err
	}
	fmt.Printf("%v\n", status)

	commit, err := w.Commit("Initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "John Doe",
			Email: "john@doe.org",
			When:  time.Time{},
		},
	})
	if err != nil {
		return err
	}
	obj, err := repo.CommitObject(commit)
	if err != nil {
		return err
	}
	fmt.Printf("%v\n", obj)

	return nil
}
