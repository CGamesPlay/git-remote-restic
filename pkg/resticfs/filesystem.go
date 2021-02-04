package resticfs

import (
	"context"
	"errors"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	billyutil "github.com/go-git/go-billy/v5/util"
	"github.com/restic/chunker"
	"github.com/restic/restic/lib/restic"
)

// blobCacheSize specifies the maximum size in bytes of the blob cache.
// Currently hardcoded to 64 MiB.
const blobCacheSize = 64 << 20

var uid, gid uint32
var userName, groupName, hostname string

// ErrNoChanges indicates that a snapshot was not created because it would be
// identical to the parent snapshot.
var ErrNoChanges = errors.New("no changes to commit")

func init() {
	uid = uint32(os.Getuid())
	u, err := user.Current()
	if err == nil {
		userName = u.Username
	}

	gid = uint32(os.Getgid())
	g, err := user.LookupGroupId(strconv.Itoa(int(gid)))
	if err == nil {
		groupName = g.Name
	}

	hostname, _ = os.Hostname()
}

// Filesystem satisfies billy.Filesystem and allows reading and writing restic
// snapshots. By default, Filesystems are read-only, writing can be enabled
// using the StartNewSnapshot method.
type Filesystem struct {
	mu sync.Mutex
	// We keep a context to pass to restic because the billy.Filesystem
	// interface doesn't provide one for operations.
	ctx       context.Context
	repo      restic.Repository
	writable  bool
	root      *resticTree
	blobCache *blobCache
	// Temporary is the backing store for temporary files created by the
	// Filesystem. The default value for Temporary is an osfs.FileSystem, but a
	// custom value can be provided here.
	Temporary billy.Filesystem
	// Logger can be provided to enable detailed logging of operations.
	Logger  *log.Logger
	chunker *chunker.Chunker
	buf     []byte
}

var _ billy.Basic = (*Filesystem)(nil)
var _ billy.Dir = (*Filesystem)(nil)
var _ billy.TempFile = (*Filesystem)(nil)

// New returns a new, read-only Filesystem based on the provided
// restic.Repository and snapshot ID. If the snapshot ID is nil, the Filesystem
// will be initially empty. The caller is responsible for properly locking and
// unlocking the restic repository.
func New(ctx context.Context, repo restic.Repository, parentSnapshotID *restic.ID) (*Filesystem, error) {
	fs := &Filesystem{
		ctx:       ctx,
		repo:      repo,
		blobCache: newBlobCache(blobCacheSize),
		Temporary: osfs.New(""),
	}
	if parentSnapshotID != nil {
		snapshot, err := restic.LoadSnapshot(ctx, repo, *parentSnapshotID)
		if err != nil {
			return nil, err
		}
		fs.root, err = openTree(fs, nil, *snapshot.Tree)
		if err != nil {
			return nil, err
		}
	} else {
		fs.root = newTree(fs, nil)
	}
	return fs, nil
}

// StartNewSnapshot enables writing to this Filesystem.  Writing to files is
// accomplished using Temporary, and only when the file
// is closed is the data actually written to the restic repository. Further,
// the data will be orphaned until the CommitSnapshot method is called.
func (fs *Filesystem) StartNewSnapshot() {
	if fs.Logger != nil {
		fs.Logger.Printf("StartNewSnapshot()\n")
	}
	fs.writable = true
}

// CommitSnapshot commits all pending changes to restic, then saves the
// resulting as a tree as a new snapshot. May return ErrNoChanges if commiting
// a snapshot would be redundant.
func (fs *Filesystem) CommitSnapshot(gitDir string, tags []string) (id restic.ID, err error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.Logger != nil {
		defer func() {
			var val interface{}
			if err != nil {
				val = err
			} else {
				val = &id
			}
			fs.Logger.Printf("CommitSnapshot() => %v\n", val)
		}()
	}
	if !fs.root.IsDirty() {
		return restic.ID{}, ErrNoChanges
	}
	var tree restic.ID
	var snapshot *restic.Snapshot
	tree, err = fs.root.Commit()
	if err != nil {
		return restic.ID{}, err
	}
	err = fs.repo.Flush(fs.ctx)
	if err != nil {
		return restic.ID{}, err
	}
	snapshot, err = restic.NewSnapshot([]string{gitDir}, tags, hostname, time.Now())
	if err != nil {
		return restic.ID{}, err
	}
	snapshot.Tree = &tree
	return fs.repo.SaveJSONUnpacked(fs.ctx, restic.SnapshotFile, snapshot)
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
func (fs *Filesystem) OpenFile(fullpath string, flag int, perm os.FileMode) (file billy.File, err error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.Logger != nil {
		defer func() {
			fs.Logger.Printf("OpenFile(%#v, %x, 0%03o) => %v\n", fullpath, flag, perm, err)
		}()
	}
	dir, filename := filepath.Split(fullpath)
	var tree *resticTree
	tree, err = fs.getTree(dir)
	if err != nil {
		return nil, err
	}
	file, err = tree.OpenFile(fullpath, filename, flag, perm)
	return file, err
}

// Stat returns a FileInfo describing the named file.
func (fs *Filesystem) Stat(fullpath string) (fi os.FileInfo, err error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.Logger != nil {
		defer func() {
			var val interface{}
			if err != nil {
				val = err
			} else {
				val = fi
			}
			fs.Logger.Printf("Stat(%#v) => %v\n", fullpath, val)
		}()
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
	return NodeInfo{node}, nil
}

// Rename renames (moves) oldpath to newpath. If newpath already exists and
// is not a directory, Rename replaces it. OS-specific restrictions may
// apply when oldpath and newpath are in different directories.
func (fs *Filesystem) Rename(oldpath, newpath string) (err error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.Logger != nil {
		defer func() {
			fs.Logger.Printf("Rename(%#v, %#v) => %v\n", oldpath, newpath, err)
		}()
	}
	var oldtree, newtree *resticTree
	olddir, oldname := filepath.Split(oldpath)
	oldtree, err = fs.getTree(olddir)
	if err != nil {
		return err
	}
	node := oldtree.Find(oldname)
	if node == nil {
		return os.ErrNotExist
	}
	newdir, newname := filepath.Split(newpath)
	newtree, err = fs.getTree(newdir)
	if err != nil {
		return err
	}
	return node.Rename(newtree, newname)
}

// Remove removes the named file or directory.
func (fs *Filesystem) Remove(fullpath string) (err error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.Logger != nil {
		defer func() {
			fs.Logger.Printf("Remove(%#v) => %v\n", fullpath, err)
		}()
	}
	dir, filename := filepath.Split(fullpath)
	var tree *resticTree
	tree, err = fs.getTree(dir)
	if err != nil {
		return err
	}
	node := tree.Find(filename)
	if node == nil {
		return os.ErrNotExist
	}
	tree.Remove(filename)
	return nil
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
func (fs *Filesystem) ReadDir(path string) (result []os.FileInfo, err error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.Logger != nil {
		defer func() {
			var val interface{}
			if err != nil {
				val = err
			} else {
				val = result
			}
			fs.Logger.Printf("ReadDir(%#v) => %v\n", path, val)
		}()
	}
	var tree *resticTree
	tree, err = fs.getTree(path)
	if err != nil {
		return nil, err
	}
	result = make([]os.FileInfo, len(tree.Nodes))
	for i, node := range tree.Nodes {
		result[i] = NodeInfo{node}
	}
	return result, nil
}

// MkdirAll creates a directory named path, along with any necessary
// parents, and returns nil, or else returns an error. The permission bits
// perm are used for all directories that MkdirAll creates. If path is/
// already a directory, MkdirAll does nothing and returns nil.
func (fs *Filesystem) MkdirAll(path string, perm os.FileMode) (err error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	components := strings.Split(filepath.Clean(path), string(os.PathSeparator))
	tree := fs.root
	for _, component := range components {
		if component == "" || component == "." {
			continue
		}
		tree, err = tree.OpenSubtree(component, os.O_CREATE, perm)
		if err != nil {
			break
		}
	}
	if fs.Logger != nil {
		fs.Logger.Printf("MkdirAll(%#v, 0%03o) => %v\n", path, perm, err)
	}
	return err
}

// TempFile creates a new temporary file in the directory dir with a name
// beginning with prefix, opens the file for reading and writing, and
// returns the resulting *os.File. If dir is the empty string, TempFile
// uses the default directory for temporary files (see os.TempDir).
// Multiple programs calling TempFile simultaneously will not choose the
// same file. The caller can use f.Name() to find the pathname of the file.
// It is the caller's responsibility to remove the file when no longer
// needed.
func (fs *Filesystem) TempFile(dir, prefix string) (billy.File, error) {
	if !fs.writable {
		return nil, os.ErrPermission
	}
	return billyutil.TempFile(fs, dir, prefix)
}

func (fs *Filesystem) getTree(path string) (*resticTree, error) {
	components := strings.Split(filepath.Clean(path), string(os.PathSeparator))
	tree := fs.root
	for _, component := range components {
		if component == "" || component == "." {
			continue
		}
		var err error
		tree, err = tree.OpenSubtree(component, 0, 0)
		if err != nil {
			return nil, err
		}
	}
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
type NodeInfo struct{ *resticNode }

// Name satisfies os.FileInfo
func (n NodeInfo) Name() string {
	return n.resticNode.Name
}

// Size satisfies os.FileInfo
func (n NodeInfo) Size() int64 {
	return int64(n.resticNode.Size)
}

// Mode satisfies os.FileInfo
func (n NodeInfo) Mode() os.FileMode {
	return n.resticNode.Mode
}

// ModTime satisfies os.FileInfo
func (n NodeInfo) ModTime() time.Time {
	return n.resticNode.ModTime
}

// IsDir satisfies os.FileInfo
func (n NodeInfo) IsDir() bool {
	return n.resticNode.Type == "dir"
}

// Sys satisfies os.FileInfo
func (n NodeInfo) Sys() interface{} {
	return nil
}
