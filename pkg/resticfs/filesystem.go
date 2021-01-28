package resticfs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/restic/restic/lib/restic"
)

// blobCacheSize specifies the maximum size in bytes of the blob cache.
// Currently hardcoded to 64 MiB.
const blobCacheSize = 64 << 20

// Filesystem satisfies billy.Filesystem and allows reading and writing restic
// snapshots. By default, Filesystems are read-only, writing can be enabled
// using the EnableCopyOnWrite method.
type Filesystem struct {
	// We keep a context to pass to restic because the billy.Filesystem
	// interface doesn't provide one for operations.
	ctx      context.Context
	repo     restic.Repository
	writable bool
	// loadedTrees is a map of restic tree IDs to the loaded representation of
	// those trees. This list is initially empty and populated on demand when
	// accessing the Filesystem.
	loadedTrees map[restic.ID]*restic.Tree
	rootTreeID  *restic.ID
	blobCache   *blobCache
}

var _ billy.Basic = (*Filesystem)(nil)
var _ billy.Dir = (*Filesystem)(nil)

// New returns a new, read-only Filesystem based on the provided
// restic.Repository and snapshot ID. If the snapshot ID is nil, the Filesystem
// will be initially empty
func New(ctx context.Context, repo restic.Repository, parentSnapshotID *restic.ID) (*Filesystem, error) {
	// XXX - need to lock the repository
	var rootTreeID *restic.ID
	if parentSnapshotID != nil {
		snapshot, err := restic.LoadSnapshot(ctx, repo, *parentSnapshotID)
		if err != nil {
			return nil, err
		}
		rootTreeID = snapshot.Tree
	}
	fs := &Filesystem{
		ctx:         ctx,
		repo:        repo,
		loadedTrees: make(map[restic.ID]*restic.Tree, 1),
		rootTreeID:  rootTreeID,
		blobCache:   newBlobCache(blobCacheSize),
	}
	return fs, nil
}

// EnableCopyOnWrite enables writing to this Filesystem. Writing to files is
// accomplished entirely in memory, and only when the file is closed is the
// data actually written to the restic repository. Further, the data will be
// orphaned until the FinalizeSnapshot method is called.
func (fs *Filesystem) EnableCopyOnWrite() {
	fs.writable = true
}

// FinalizeSnapshot isn't implemented. We probably want to actually update the
// existing snapshot here.
func (fs *Filesystem) FinalizeSnapshot() {
	panic("FinalizeSnapshot not implemented")
}

// Create creates the named file with mode 0666 (before umask), truncating
// it if it already exists. If successful, methods on the returned File can
// be used for I/O; the associated file descriptor has mode O_RDWR.
func (fs *Filesystem) Create(filename string) (billy.File, error) {
	return fs.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
}

// Open opens the named file for reading. If successful, methods on the
// returned file can be used for reading; the associated file descriptor has
// mode O_RDONLY.
func (fs *Filesystem) Open(filename string) (billy.File, error) {
	return fs.OpenFile(filename, os.O_RDONLY, 0)
}

// OpenFile is the generalized open call; most users will use Open or Create
// instead. It opens the named file with specified flag (O_RDONLY etc.) and
// perm, (0666 etc.) if applicable. If successful, methods on the returned
// File can be used for I/O.
func (fs *Filesystem) OpenFile(fullpath string, flag int, perm os.FileMode) (billy.File, error) {
	if flag&os.O_RDWR != 0 || flag&os.O_WRONLY != 0 {
		return nil, os.ErrPermission
	}
	dir, filename := filepath.Split(fullpath)
	tree, err := fs.getTree(dir)
	if err != nil {
		return nil, err
	}
	node := tree.Find(filename)
	if node == nil {
		return nil, os.ErrNotExist
	}
	return newFile(fs, node)
}

// Stat returns a FileInfo describing the named file.
func (fs *Filesystem) Stat(fullpath string) (os.FileInfo, error) {
	dir, filename := filepath.Split(fullpath)
	tree, err := fs.getTree(dir)
	if err != nil {
		return nil, err
	}
	node := tree.Find(filename)
	if node == nil {
		return nil, os.ErrNotExist
	}
	return NodeInfo{node}, nil
}

// Rename renames (moves) oldpath to newpath. If newpath already exists and
// is not a directory, Rename replaces it. OS-specific restrictions may
// apply when oldpath and newpath are in different directories.
func (fs *Filesystem) Rename(oldpath, newpath string) error {
	panic("Rename not implemented")
}

// Remove removes the named file or directory.
func (fs *Filesystem) Remove(filename string) error {
	panic("Remove not implemented")
}

// Join joins any number of path elements into a single path, adding a
// Separator if necessary. Join calls filepath.Clean on the result; in
// particular, all empty strings are ignored. On Windows, the result is a
// UNC path if and only if the first path element is a UNC path.
func (fs *Filesystem) Join(elem ...string) string {
	return filepath.Join(elem...)
}

// ReadDir reads the directory named by dirname and returns a list of
// directory entries sorted by filename.
func (fs *Filesystem) ReadDir(path string) ([]os.FileInfo, error) {
	tree, err := fs.getTree(path)
	if err != nil {
		return nil, err
	}
	result := make([]os.FileInfo, len(tree.Nodes))
	for i, node := range tree.Nodes {
		result[i] = NodeInfo{node}
	}
	return result, nil
}

// MkdirAll creates a directory named path, along with any necessary
// parents, and returns nil, or else returns an error. The permission bits
// perm are used for all directories that MkdirAll creates. If path is/
// already a directory, MkdirAll does nothing and returns nil.
func (fs *Filesystem) MkdirAll(filename string, perm os.FileMode) error {
	panic("MkDirAll not implemented")
}

func (fs *Filesystem) getTree(path string) (*restic.Tree, error) {
	components := strings.Split(filepath.Clean(path), string(os.PathSeparator))
	tree, err := fs.loadTreeByID(fs.rootTreeID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "root tree %v not found\n", fs.rootTreeID)
		return nil, err
	}
	for _, component := range components {
		if component == "" || component == "." {
			continue
		}
		node := tree.Find(component)
		if node == nil {
			return nil, os.ErrNotExist
		}
		if node.Type != "dir" {
			return nil, os.ErrNotExist
		}
		tree, err = fs.loadTreeByID(node.Subtree)
		if err != nil {
			return nil, err
		}
	}
	return tree, nil
}

// loadTree loads the tree specified by ID and populates the cache.
func (fs *Filesystem) loadTreeByID(id *restic.ID) (*restic.Tree, error) {
	if tree, ok := fs.loadedTrees[*id]; ok {
		return tree, nil
	}
	tree, err := fs.repo.LoadTree(fs.ctx, *id)
	if err != nil {
		return nil, err
	}
	fs.loadedTrees[*id] = tree
	return tree, nil
}

func (fs *Filesystem) getBlob(id restic.ID) ([]byte, error) {
	blob, ok := fs.blobCache.get(id)
	if ok {
		return blob, nil
	}
	blob, err := fs.repo.LoadBlob(fs.ctx, restic.DataBlob, id, nil)
	if err != nil {
		return nil, err
	}
	fs.blobCache.add(id, blob)
	return blob, nil
}

// NodeInfo satisfies os.FileInfo for a *restic.Node.
type NodeInfo struct{ *restic.Node }

// Name satisfies os.FileInfo
func (n NodeInfo) Name() string {
	return n.Node.Name
}

// Size satisfies os.FileInfo
func (n NodeInfo) Size() int64 {
	return int64(n.Node.Size)
}

// Mode satisfies os.FileInfo
func (n NodeInfo) Mode() os.FileMode {
	return n.Node.Mode
}

// ModTime satisfies os.FileInfo
func (n NodeInfo) ModTime() time.Time {
	return n.Node.ModTime
}

// IsDir satisfies os.FileInfo
func (n NodeInfo) IsDir() bool {
	return n.Node.Type == "dir"
}

// Sys satisfies os.FileInfo
func (n NodeInfo) Sys() interface{} {
	return nil
}
