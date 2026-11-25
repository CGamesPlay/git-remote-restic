package main

import (
	"context"

	"github.com/CGamesPlay/git-remote-restic/pkg/resticfs"
	"github.com/go-git/go-billy/v5/helper/polyfill"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/cache"
	gitfs "github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/pkg/errors"
	"github.com/restic/restic/lib/backend/all"
	"github.com/restic/restic/lib/data"
	"github.com/restic/restic/lib/global"
	"github.com/restic/restic/lib/repository"
	"github.com/restic/restic/lib/restic"
	"github.com/restic/restic/lib/ui"
	"github.com/restic/restic/lib/ui/progress"
)

// Repository is a wrapper around a restic-backed git repository.
type Repository struct {
	restic  *repository.Repository
	printer restic.Printer
	git     *git.Repository
	fs      *resticfs.Filesystem
}

// NewRepository creates a new Repository. The terminal must write only to
// stderr, since stdout carries the remote-helper protocol.
func NewRepository(ctx context.Context, path string, password string, term ui.Terminal) (*Repository, error) {
	globalOptions := global.Options{
		Repo:        path,
		NoCache:     true,
		Compression: repository.CompressionOff,
		Backends:    all.Backends(),
		Password:    password,
		Term:        term,
	}
	printer := progress.NewTerminalPrinter(false, 0, term)

	resticRepo, err := global.OpenRepository(ctx, globalOptions, printer)
	if err != nil {
		return nil, err
	}

	if err = resticRepo.LoadIndex(ctx, printer); err != nil {
		return nil, err
	}

	return &Repository{restic: resticRepo, printer: printer}, nil
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
		f := data.SnapshotFilter{}
		sn, _, err := f.FindLatest(context.Background(), r.restic, r.restic, "latest")
		if err != nil && !errors.Is(err, data.ErrNoSnapshotFound) {
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

// Lock creates the listed type of lock on the repository,
// which restic continually refreshes in the background until the returned (`unlock`) function is invoked.
func (r *Repository) Lock(exclusive bool) (unlock func(), err error) {
	ctx := context.Background()

	unlock, _, err = repository.LockRepo(ctx, r.restic, exclusive, 0, func(msg string) {
		r.printer.P("%s", msg)
	}, r.printer.E)
	if err != nil {
		return nil, errors.WithMessage(err, "unable to create lock in backend")
	}

	return unlock, nil
}
