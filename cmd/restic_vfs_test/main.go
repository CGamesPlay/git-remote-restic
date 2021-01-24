package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/CGamesPlay/git-remote-restic/pkg/filesystem"
	"github.com/go-git/go-billy/v5/helper/polyfill"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	gitfs "github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/restic/restic/lib/backend"
	"github.com/restic/restic/lib/backend/local"
	"github.com/restic/restic/lib/limiter"
	"github.com/restic/restic/lib/repository"
	"github.com/restic/restic/lib/restic"
)

var resticRepo *repository.Repository

func openRepository(path string, password string) (*repository.Repository, error) {
	var err error
	ctx := context.Background()
	config, err := local.ParseConfig(path)
	if err != nil {
		return nil, err
	}
	var be restic.Backend
	if be, err = local.Open(ctx, config.(local.Config)); err != nil {
		return nil, err
	}
	lim := limiter.NewStaticLimiter(0, 0)
	be = limiter.LimitBackend(be, lim)
	be = backend.NewRetryBackend(be, 10, func(msg string, err error, d time.Duration) {
		fmt.Fprintf(os.Stderr, "%v returned error, retrying after %v: %v\n", msg, d, err)
	})
	repo := repository.New(be)
	if err = repo.SearchKey(ctx, password, 0, ""); err != nil {
		return nil, err
	}

	if err = repo.LoadIndex(ctx); err != nil {
		return nil, err
	}

	return repo, err
}

func testRestic(ctx context.Context) error {
	id, err := restic.FindLatestSnapshot(ctx, resticRepo, nil, nil, nil)
	if err != nil {
		return err
	}
	snapshot, err := restic.LoadSnapshot(ctx, resticRepo, id)
	if err != nil {
		return err
	}
	fs, err := filesystem.NewResticTreeFs(ctx, resticRepo, snapshot.Tree)
	if err != nil {
		return err
	}
	pf := polyfill.New(fs)
	s := gitfs.NewStorageWithOptions(pf, cache.NewObjectLRUDefault(), gitfs.Options{KeepDescriptors: true})
	repo, err := git.Open(s, pf)
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

func main() {
	var err error
	resticRepo, err = openRepository("local:fixtures/restic", "password")
	if err != nil {
		panic(err)
	}
	err = testRestic(context.Background())
	if err != nil {
		panic(err)
	}
}
