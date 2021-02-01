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
	locks         []*restic.Lock
	cancelRefresh chan struct{}
	refreshWG     sync.WaitGroup
	sync.Mutex
}

// Repository is a wrapper around a restic-backed git repository.
type Repository struct {
	restic restic.Repository
	git    *git.Repository
	fs     *resticfs.Filesystem
}

// NewRepository creates a new Repository.
func NewRepository(ctx context.Context, path string, password string) (*Repository, error) {
	be, err := openResticBackend(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	resticRepo := repository.New(be)
	if err = resticRepo.SearchKey(ctx, password, 0, ""); err != nil {
		return nil, err
	}

	if err = resticRepo.LoadIndex(ctx); err != nil {
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
		id, err := restic.FindLatestSnapshot(context.Background(), r.restic, nil, nil, nil)
		if err != nil && err != restic.ErrNoSnapshotFound {
			return nil, err
		}
		if err == nil {
			parentSnapshot = &id
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
func (r *Repository) Lock(exclusive bool) (*restic.Lock, error) {
	ctx := context.Background()
	lockFn := restic.NewLock
	if exclusive {
		lockFn = restic.NewExclusiveLock
	}

	lock, err := lockFn(ctx, r.restic)
	if err != nil {
		return nil, errors.WithMessage(err, "unable to create lock in backend")
	}

	globalLocks.Lock()
	if globalLocks.cancelRefresh == nil {
		globalLocks.cancelRefresh = make(chan struct{})
		globalLocks.refreshWG = sync.WaitGroup{}
		globalLocks.refreshWG.Add(1)
		go refreshLocks(&globalLocks.refreshWG, globalLocks.cancelRefresh)
	}

	globalLocks.locks = append(globalLocks.locks, lock)
	globalLocks.Unlock()

	return lock, err
}

// Unlock unlocks the provided lock.
func (r *Repository) Unlock(lock *restic.Lock) {
	if lock == nil {
		return
	}

	globalLocks.Lock()
	defer globalLocks.Unlock()

	for i := 0; i < len(globalLocks.locks); i++ {
		if lock == globalLocks.locks[i] {
			// remove the lock from the repo
			if err := lock.Unlock(); err != nil {
				Warnf("error while unlocking: %v", err)
				return
			}

			// remove the lock from the list of locks
			globalLocks.locks = append(globalLocks.locks[:i], globalLocks.locks[i+1:]...)
			return
		}
	}
}

func refreshLocks(wg *sync.WaitGroup, done <-chan struct{}) {
	defer func() {
		wg.Done()
		globalLocks.Lock()
		globalLocks.cancelRefresh = nil
		globalLocks.Unlock()
	}()

	ticker := time.NewTicker(lockRefreshInterval)

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			globalLocks.Lock()
			for _, lock := range globalLocks.locks {
				err := lock.Refresh(context.TODO())
				if err != nil {
					Warnf("unable to refresh lock: %v\n", err)
				}
			}
			globalLocks.Unlock()
		}
	}
}
