package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/CGamesPlay/git-remote-restic/pkg/resticfs"
	"github.com/restic/restic/lib/backend"
	"github.com/restic/restic/lib/backend/local"
	"github.com/restic/restic/lib/limiter"
	"github.com/restic/restic/lib/repository"
	"github.com/restic/restic/lib/restic"
)

func openRepository(path string, password string) (*resticfs.Filesystem, error) {
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

	id, err := restic.FindLatestSnapshot(ctx, repo, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	fs, err := resticfs.New(ctx, repo, &id)
	if err != nil {
		return nil, err
	}

	return fs, err
}

func testRestic(fs *resticfs.Filesystem) error {
	items, err := fs.ReadDir("")
	if err != nil {
		return err
	}
	for _, fi := range items {
		fmt.Printf("%v\n", fi)
	}
	file, err := fs.Open("README.md")
	if err != nil {
		return err
	}
	data, err := ioutil.ReadAll(file)
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	fmt.Print(string(data))
	return nil
}

func main() {
	fs, err := openRepository("local:fixtures/basic", "password")
	if err != nil {
		panic(err)
	}
	err = testRestic(fs)
	if err != nil {
		panic(err)
	}
}
