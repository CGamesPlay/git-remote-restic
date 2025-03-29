package main

import (
	"context"
	"sync"
	"time"

	"github.com/CGamesPlay/git-remote-restic/pkg/resticfs"
	"github.com/go-git/go-billy/v5/helper/polyfill"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/cache"
	gitfs "github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/pkg/errors"
	"github.com/restic/restic/lib/repository"
	"github.com/restic/restic/lib/restic"
)

const lockRefreshInterval = 5 * time.Minute

var globalLocks struct {
	locks         []*repository.Unlocker
	cancelRefresh chan struct{}
	refreshWG     sync.WaitGroup
	sync.Mutex
}

// Repository is a wrapper around a restic-backed git repository.
type Repository struct {
	restic *repository.Repository
	git    *git.Repository
	fs     *resticfs.Filesystem
}

// NewRepository creates a new Repository.
func NewRepository(ctx context.Context, path string, password string) (*Repository, error) {
	// This code inspired by restic/cmd/restic/global.go OpenRepository.
	be, err := open(ctx, path, globalOptions, globalOptions.extended)
	if err != nil {
		return nil, err
	}
	resticRepo, err := repository.New(be, repository.Options{
		Compression: repository.CompressionOff,
		PackSize:    0,
	})
	if err != nil {
		return nil, err
	}
	if err = resticRepo.SearchKey(ctx, password, 0, ""); err != nil {
		return nil, err
	}

	if err = resticRepo.LoadIndex(ctx, nil); err != nil {
		return nil, err
	}

	repo := &Repository{
		restic: resticRepo,
	}

	return repo, err
}

// Git returns the *git.Repository stored in the restic.Repository. If no such
// repository exists, one will be created if allowInit is true.
func (r *Repository) Git(allowInit bool) (*git.Repository, error) {
	if r.git != nil {
		return r.git, nil
	}
	var err error
	if r.fs == nil {
		var parentSnapshot *restic.ID
		f := restic.SnapshotFilter{}
		sn, _, err := f.FindLatest(context.Background(), r.restic, r.restic, "latest")
		if err != nil && !errors.Is(err, restic.ErrNoSnapshotFound) {
			return nil, err
		}
		if err == nil {
			parentSnapshot = sn.ID()
		}
		r.fs, err = resticfs.New(context.Background(), r.restic, parentSnapshot)
		if err != nil {
			return nil, err
		}
		//r.fs.Logger = log.New(os.Stderr, "resticfs: ", 0)
	}
	pf := polyfill.New(r.fs)
	s := gitfs.NewStorageWithOptions(pf, cache.NewObjectLRUDefault(), gitfs.Options{KeepDescriptors: true})
	r.git, err = git.Open(s, nil)
	if err == git.ErrRepositoryNotExists && allowInit {
		r.git, err = git.Init(s, nil)
	}
	return r.git, err
}

// Lock creates the listed type of lock on the repository, and uses a goroutine
// to ensure that the lock doesn't expire.
func (r *Repository) Lock(exclusive bool) (*repository.Unlocker, error) {
	ctx := context.Background()

	lock, _, err := repository.Lock(ctx, r.restic, exclusive, globalOptions.RetryLock, func(msg string) {
		Verbosef("%s", msg)
	}, Warnf)
	if err != nil {
		return nil, errors.WithMessage(err, "unable to create lock in backend")
	}

	return lock, err
}

// Unlock unlocks the provided lock.
func (r *Repository) Unlock(lock *repository.Unlocker) {
	if lock == nil {
		return
	}
	lock.Unlock()
}
