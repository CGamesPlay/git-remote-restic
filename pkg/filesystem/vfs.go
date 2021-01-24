package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/restic/restic/lib/restic"
)

// ErrReadOnlyFilesystem is returned when any operation attempts to modify this
// filesystem.
var ErrReadOnlyFilesystem = errors.New("read-only filesystem")

// ResticTreeFs implements billy.Filesystem for a particular restic.Tree. This
// filesystem is read-only.
type ResticTreeFs struct {
	// We keep a context to pass to restic because the billy.Filesystem
	// interface doesn't provide one for operations.
	ctx  context.Context
	repo restic.Repository
	// trees is a map of pathname to loaded Tree object. This is a cache with
	// no eviction policy, aka a memory leak, however the snapshots for git
	// repos only have a few hundred directories, maximum.
	trees map[string]*restic.Tree
}

var _ billy.Basic = (*ResticTreeFs)(nil)
var _ billy.Dir = (*ResticTreeFs)(nil)

// NewResticTreeFs creates a new ResticTreeFs using the provided repository and
// tree ID. The provided context must last for the lifetime of the
// ResticTreeFs. Once it is canceled all operations on the filesystem will
// fail.
func NewResticTreeFs(ctx context.Context, repo restic.Repository, id *restic.ID) (*ResticTreeFs, error) {
	tree, err := repo.LoadTree(ctx, *id)
	if err != nil {
		return nil, err
	}
	trees := map[string]*restic.Tree{
		"": tree,
	}
	fs := &ResticTreeFs{ctx, repo, trees}
	return fs, nil
}

// Create creates the named file with mode 0666 (before umask), truncating
// it if it already exists. If successful, methods on the returned File can
// be used for I/O; the associated file descriptor has mode O_RDWR.
func (fs *ResticTreeFs) Create(filename string) (billy.File, error) {
	return fs.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
}

// Open opens the named file for reading. If successful, methods on the
// returned file can be used for reading; the associated file descriptor has
// mode O_RDONLY.
func (fs *ResticTreeFs) Open(filename string) (billy.File, error) {
	return fs.OpenFile(filename, os.O_RDONLY, 0)
}

// OpenFile is the generalized open call; most users will use Open or Create
// instead. It opens the named file with specified flag (O_RDONLY etc.) and
// perm, (0666 etc.) if applicable. If successful, methods on the returned
// File can be used for I/O.
func (fs *ResticTreeFs) OpenFile(fullpath string, flag int, perm os.FileMode) (billy.File, error) {
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
	return newResticFile(fs, node)
}

// Stat returns a FileInfo describing the named file.
func (fs *ResticTreeFs) Stat(fullpath string) (os.FileInfo, error) {
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
func (fs *ResticTreeFs) Rename(oldpath, newpath string) error {
	return os.ErrPermission
}

// Remove removes the named file or directory.
func (fs *ResticTreeFs) Remove(filename string) error {
	return os.ErrPermission
}

// Join joins any number of path elements into a single path, adding a
// Separator if necessary. Join calls filepath.Clean on the result; in
// particular, all empty strings are ignored. On Windows, the result is a
// UNC path if and only if the first path element is a UNC path.
func (fs *ResticTreeFs) Join(elem ...string) string {
	return filepath.Join(elem...)
}

// ReadDir reads the directory named by dirname and returns a list of
// directory entries sorted by filename.
func (fs *ResticTreeFs) ReadDir(path string) ([]os.FileInfo, error) {
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
func (fs *ResticTreeFs) MkdirAll(filename string, perm os.FileMode) error {
	return os.ErrPermission
}

func (fs *ResticTreeFs) getTree(path string) (*restic.Tree, error) {
	if len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}
	if len(path) > 0 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	if tree, ok := fs.trees[path]; ok {
		return tree, nil
	}
	if len(path) == 0 {
		panic("root tree missing")
	}
	dir, file := filepath.Split(path)
	parent, err := fs.getTree(dir)
	if err != nil {
		return nil, err
	}
	node := parent.Find(file)
	if node == nil {
		return nil, os.ErrNotExist
	}
	if node.Type != "dir" {
		return nil, os.ErrNotExist
	}
	tree, err := fs.repo.LoadTree(fs.ctx, *node.Subtree)
	if err != nil {
		return nil, err
	}
	fs.trees[path] = tree
	return tree, nil
}

func (fs *ResticTreeFs) getBlob(id restic.ID) ([]byte, error) {
	// TODO - implement a cache
	blob, err := fs.repo.LoadBlob(fs.ctx, restic.DataBlob, id, nil)
	if err != nil {
		return nil, err
	}
	return blob, nil
}

type resticFile struct {
	fs   *ResticTreeFs
	node *restic.Node
	// cumsize holds the cumulatoive size of blobs[:i]
	cumsize  []uint64
	isClosed bool
	position int64
}

var _ billy.File = (*resticFile)(nil)

func newResticFile(fs *ResticTreeFs, node *restic.Node) (*resticFile, error) {
	file := &resticFile{
		fs:      fs,
		node:    node,
		cumsize: make([]uint64, len(node.Content)+1),
	}
	acc := uint64(0)
	for i, id := range node.Content {
		size, found := fs.repo.LookupBlobSize(id, restic.DataBlob)
		if !found {
			return nil, fmt.Errorf("id %v not found in repository", id)
		}
		acc += uint64(size)
		file.cumsize[i+1] = acc
	}
	if acc != node.Size {
		return nil, fmt.Errorf("incorrect size on %v", node.Name)
	}
	return file, nil
}

func (f *resticFile) Name() string {
	return f.node.Name
}

func (f *resticFile) Lock() error {
	// Locking is meaningless on a read-only filesystem
	return nil
}

func (f *resticFile) Unlock() error {
	return nil
}

func (f *resticFile) Truncate(size int64) error {
	return ErrReadOnlyFilesystem
}

func (f *resticFile) Close() error {
	if f.isClosed {
		return os.ErrClosed
	}
	f.isClosed = true
	return nil
}

func (f *resticFile) Write(p []byte) (int, error) {
	if f.isClosed {
		return 0, os.ErrClosed
	}
	return 0, ErrReadOnlyFilesystem
}

func (f *resticFile) Read(b []byte) (int, error) {
	n, err := f.ReadAt(b, f.position)
	f.position += int64(n)
	if err == io.EOF && n != 0 {
		err = nil
	}
	return n, err
}

func (f *resticFile) ReadAt(b []byte, off int64) (int, error) {
	if f.isClosed {
		return 0, os.ErrClosed
	}
	offset := uint64(off)
	// This method mostly comes from restic/fuse/file.go
	startContent := -1 + sort.Search(len(f.cumsize), func(i int) bool {
		return f.cumsize[i] > offset
	})
	offset -= f.cumsize[startContent]

	readBytes := 0
	remainingBytes := len(b)
	for i := startContent; remainingBytes > 0 && i < len(f.cumsize)-1; i++ {
		blob, err := f.fs.getBlob(f.node.Content[i])
		if err != nil {
			return readBytes, err
		}
		if offset > 0 {
			blob = blob[offset:]
			offset = 0
		}
		copied := copy(b, blob)
		remainingBytes -= copied
		readBytes += copied
		b = b[copied:]
	}
	if remainingBytes > 0 {
		return readBytes, io.EOF
	}
	return readBytes, nil
}

func (f *resticFile) Seek(offset int64, whence int) (int64, error) {
	if f.isClosed {
		return 0, os.ErrClosed
	}

	switch whence {
	case io.SeekCurrent:
		f.position += offset
	case io.SeekStart:
		f.position = offset
	case io.SeekEnd:
		f.position = int64(f.cumsize[len(f.cumsize)-1]) + offset
	}

	return f.position, nil
}

func (f *resticFile) getBlobAt(i int) ([]byte, error) {
	panic("not implemented")
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
