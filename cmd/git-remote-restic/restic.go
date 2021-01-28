package main

import (
	"context"
	"sync"
	"time"

	"github.com/restic/restic/lib/backend"
	"github.com/restic/restic/lib/backend/local"
	"github.com/restic/restic/lib/debug"
	"github.com/restic/restic/lib/errors"
	"github.com/restic/restic/lib/limiter"
	"github.com/restic/restic/lib/repository"
	"github.com/restic/restic/lib/restic"
)

func openRepository(path string) (*repository.Repository, error) {
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
		Warnf("%v returned error, retrying after %v: %v\n", msg, d, err)
	})
	repo := repository.New(be)
	password := "password"
	if err = repo.SearchKey(ctx, password, 0, ""); err != nil {
		return nil, err
	}

	if err = repo.LoadIndex(ctx); err != nil {
		return nil, err
	}

	return repo, err
}

var globalLocks struct {
	locks         []*restic.Lock
	cancelRefresh chan struct{}
	refreshWG     sync.WaitGroup
	sync.Mutex
}

func lockRepository(ctx context.Context, repo restic.Repository, exclusive bool) (*restic.Lock, error) {
	lockFn := restic.NewLock
	if exclusive {
		lockFn = restic.NewExclusiveLock
	}

	lock, err := lockFn(ctx, repo)
	if err != nil {
		return nil, errors.WithMessage(err, "unable to create lock in backend")
	}
	debug.Log("create lock %p (exclusive %v)", lock, exclusive)

	globalLocks.Lock()
	if globalLocks.cancelRefresh == nil {
		debug.Log("start goroutine for lock refresh")
		globalLocks.cancelRefresh = make(chan struct{})
		globalLocks.refreshWG = sync.WaitGroup{}
		globalLocks.refreshWG.Add(1)
		go refreshLocks(&globalLocks.refreshWG, globalLocks.cancelRefresh)
	}

	globalLocks.locks = append(globalLocks.locks, lock)
	globalLocks.Unlock()

	return lock, err
}

var refreshInterval = 5 * time.Minute

func refreshLocks(wg *sync.WaitGroup, done <-chan struct{}) {
	debug.Log("start")
	defer func() {
		wg.Done()
		globalLocks.Lock()
		globalLocks.cancelRefresh = nil
		globalLocks.Unlock()
	}()

	ticker := time.NewTicker(refreshInterval)

	for {
		select {
		case <-done:
			debug.Log("terminate")
			return
		case <-ticker.C:
			debug.Log("refreshing locks")
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

func unlockRepo(lock *restic.Lock) {
	if lock == nil {
		return
	}

	globalLocks.Lock()
	defer globalLocks.Unlock()

	for i := 0; i < len(globalLocks.locks); i++ {
		if lock == globalLocks.locks[i] {
			// remove the lock from the repo
			debug.Log("unlocking repository with lock %v", lock)
			if err := lock.Unlock(); err != nil {
				debug.Log("error while unlocking: %v", err)
				Warnf("error while unlocking: %v", err)
				return
			}

			// remove the lock from the list of locks
			globalLocks.locks = append(globalLocks.locks[:i], globalLocks.locks[i+1:]...)
			return
		}
	}

	debug.Log("unable to find lock %v in the global list of locks, ignoring", lock)
}
