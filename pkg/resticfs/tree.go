package resticfs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/restic/chunker"
	"github.com/restic/restic/lib/restic"
)

const oWRITEABLE = os.O_RDWR | os.O_WRONLY
const uMask = os.FileMode(0002)

// ErrInUse indicates that a snapshot couldn't be made because of ongoing
// writes.
var ErrInUse = errors.New("file is currently open for writing")

// ErrNotDirectory indicates that a file is attempting to be opened as a
// directory
var ErrNotDirectory = errors.New("file is not a directory")

type resticTree struct {
	fs     *Filesystem
	parent *resticTree
	Nodes  []*resticNode
	ID     *restic.ID
}

func newTree(fs *Filesystem, parent *resticTree) *resticTree {
	return &resticTree{
		fs:     fs,
		parent: parent,
		Nodes:  make([]*resticNode, 0),
		ID:     nil,
	}
}

func openTree(fs *Filesystem, parent *resticTree, original restic.ID) (*resticTree, error) {
	tree, err := restic.LoadTree(fs.ctx, fs.repo, original)
	if err != nil {
		return nil, err
	}
	t := &resticTree{
		fs:     fs,
		parent: parent,
		Nodes:  make([]*resticNode, len(tree.Nodes)),
		ID:     &original,
	}
	for i := range tree.Nodes {
		t.Nodes[i] = newFromNode(t.fs, t, tree.Nodes[i])
	}
	return t, nil
}

func (t *resticTree) Find(name string) *resticNode {
	for _, n := range t.Nodes {
		if n.Name == name {
			return n
		}
	}
	return nil
}

// OpenSubtree is an analog to OpenFile, but for directories. The only
// supported flags are:
// - os.O_CREATE, create the directory if it does not exist
// - os.O_EXCL, fail if the directory already exists
func (t *resticTree) OpenSubtree(name string, flag int, perm os.FileMode) (*resticTree, error) {
	n := t.Find(name)
	if n == nil {
		if flag&os.O_CREATE != 0 {
			// should create new directory
			if !t.fs.writable {
				return nil, os.ErrPermission
			}
			n = newDirectory(t.fs, t, name, perm)
		} else {
			return nil, os.ErrNotExist
		}
	} else if flag&os.O_EXCL != 0 {
		return nil, os.ErrExist
	}
	return n.OpenSubtree()
}

func (t *resticTree) OpenFile(original string, name string, flag int, perm os.FileMode) (billy.File, error) {
	node := t.Find(name)
	if node == nil {
		if flag&os.O_CREATE != 0 {
			// should create empty file
			if !t.fs.writable {
				return nil, os.ErrPermission
			}
			node = newFile(t.fs, t, name, perm)
		} else {
			return nil, os.ErrNotExist
		}
	} else if flag&os.O_EXCL != 0 {
		return nil, os.ErrExist
	} else if node.Type != "file" {
		// XXX - need to handle the other node types here
		return nil, fmt.Errorf("refusing to open restic node with type %#v", node.Type)
	}
	return node.Open(original, flag, perm)
}

// Commit will persist any modifications to the restic repository.
func (t *resticTree) Commit() (restic.ID, error) {
	if t.ID != nil {
		return *t.ID, nil
	}
	tree := restic.Tree{
		Nodes: make([]*restic.Node, len(t.Nodes)),
	}
	for i, n := range t.Nodes {
		if err := n.Commit(); err != nil {
			return restic.ID{}, err
		}
		tree.Nodes[i] = &n.Node
	}
	data, err := json.Marshal(tree)
	if err != nil {
		return restic.ID{}, err
	}
	data = append(data, '\n')

	id := restic.Hash(data)
	if t.fs.repo.Index().Has(restic.BlobHandle{ID: id, Type: restic.TreeBlob}) {
		goto success
	}
	_, _, _, err = t.fs.repo.SaveBlob(t.fs.ctx, restic.TreeBlob, data, id, false)
	if err != nil {
		return restic.ID{}, err
	}

success:
	t.ID = &id
	return id, nil
}

func (t *resticTree) addNode(n *resticNode) {
	existing := t.Find(n.Node.Name)
	if existing != nil {
		// This is a panic because it's in a private interface and
		// Filesystem.OpenFile should properly handle this case.
		panic("attempt to add node with conflicting name")
	}
	t.Nodes = append(t.Nodes, n)
	t.markDirty()
}

func (t *resticTree) Remove(name string) {
	var i int
	for i = range t.Nodes {
		if t.Nodes[i].Name == name {
			break
		}
	}
	if i == len(t.Nodes) {
		return
	}
	t.Nodes[i] = t.Nodes[len(t.Nodes)-1]
	t.Nodes = t.Nodes[:len(t.Nodes)-1]
	t.markDirty()
}

func (t *resticTree) IsDirty() bool {
	return t.ID == nil
}

func (t *resticTree) markDirty() {
	for t != nil {
		t.ID = nil
		t = t.parent
	}
}

// resticNode stores information about a single file or directory. The only
// methods which are save to be called concurrently are Backing and SetBacking.
type resticNode struct {
	fs     *Filesystem
	parent *resticTree
	restic.Node
	subtree     *resticTree
	flock       sync.Mutex
	backingMu   sync.Mutex
	backing     billy.File
	openWriters int
}

func newFromNode(fs *Filesystem, parent *resticTree, node *restic.Node) *resticNode {
	n := &resticNode{fs: fs, parent: parent, Node: *node}
	return n
}

func newDirectory(fs *Filesystem, parent *resticTree, name string, perm os.FileMode) *resticNode {
	n := &resticNode{
		fs:     fs,
		parent: parent,
		Node: restic.Node{
			Name:       name,
			Type:       "dir",
			Mode:       perm & ^uMask,
			ModTime:    time.Now(),
			AccessTime: time.Now(),
			ChangeTime: time.Now(),
			UID:        uid,
			GID:        gid,
			User:       userName,
			Group:      groupName,
		},
		subtree: newTree(fs, parent),
	}
	parent.addNode(n)
	return n
}

func newFile(fs *Filesystem, parent *resticTree, name string, perm os.FileMode) *resticNode {
	n := &resticNode{
		fs:     fs,
		parent: parent,
		Node: restic.Node{
			Name:       name,
			Type:       "file",
			Mode:       perm & ^uMask,
			ModTime:    time.Now(),
			AccessTime: time.Now(),
			ChangeTime: time.Now(),
			UID:        uid,
			GID:        gid,
			User:       userName,
			Group:      groupName,
		},
	}
	parent.addNode(n)
	return n
}

// Rename moves this node into the new tree under the new name.
func (n *resticNode) Rename(newtree *resticTree, newname string) error {
	if exist := newtree.Find(newname); exist != nil {
		return os.ErrExist
	}
	if n.parent != newtree && n.parent != nil {
		n.parent.Remove(n.Node.Name)
	}
	n.Node.Name = newname
	if n.parent != newtree {
		newtree.addNode(n)
	}
	return nil
}

// Open should create a handle to the file backed by this node.
func (n *resticNode) Open(name string, flag int, perm os.FileMode) (billy.File, error) {
	if n.Backing() == nil {
		if n.Node.Content == nil {
			// This is a new, empty file. Create a temporary backing.
			backing, err := n.fs.Temporary.TempFile("", n.Node.Name)
			if err != nil {
				return nil, err
			}
			n.SetBacking(backing)
			n.markDirty()
		} else {
			// This is an exsiting file, create a read-only backing.
			backing, err := newResticFile(n.fs, n)
			if err != nil {
				return nil, err
			}
			n.SetBacking(backing)
			if flag&oWRITEABLE != 0 {
				// And make a writable backing.
				err := n.makeWritable()
				if err != nil {
					return nil, err
				}
			}
		}
	} else if flag&oWRITEABLE != 0 && n.Node.Content != nil {
		// This existing file needs to be converted to a writable one.
		err := n.makeWritable()
		if err != nil {
			return nil, err
		}
	}
	f, err := newFileHandle(n, name, flag)
	if err != nil {
		return nil, err
	}
	if flag&oWRITEABLE != 0 {
		if n.openWriters > 0 {
			// This cannot be correctly supported until the writers switch to
			// using WriteAt.
			return nil, ErrInUse
		}
		n.openWriters++
	}
	return f, nil
}

func (n *resticNode) OpenSubtree() (*resticTree, error) {
	if n.Type != "dir" {
		return nil, ErrNotDirectory
	}
	if n.subtree == nil {
		var err error
		n.subtree, err = openTree(n.fs, n.parent, *n.Node.Subtree)
		if err != nil {
			return nil, err
		}
	}
	return n.subtree, nil
}

// Backing returns the underlying datastore for the file's data. Since it is
// accessed by each file handle, it needs to be concurrency-safe. When a file
// is converted from read-only to writeable, the backing store is swapped out,
// which means that existing readers transparently start accessing the copy and
// have consistent reads.
func (n *resticNode) Backing() billy.File {
	n.backingMu.Lock()
	defer n.backingMu.Unlock()
	return n.backing
}

func (n *resticNode) SetBacking(val billy.File) {
	n.backingMu.Lock()
	defer n.backingMu.Unlock()
	n.backing = val
}

// Commit will persist any modifications to the restic repository.
func (n *resticNode) Commit() (err error) {
	if n.fs.Logger != nil {
		defer func() {
			n.fs.Logger.Printf("(*resticNode)(%p).Commit() => %v\n", n, err)
		}()
	}
	switch n.Node.Type {
	case "file":
		if n.Node.Content != nil {
			// Already committed.
			return nil
		}
		if n.openWriters > 0 {
			// The goal here is for the snapshot to be internally consistent.
			// Check how restic handles this, and possibly change this
			// behavior.
			return ErrInUse
		}
		n.Node.Size = 0
		rd := n.Backing()
		rd.Seek(0, io.SeekStart)
		if n.fs.buf == nil {
			n.fs.buf = make([]byte, chunker.MaxSize)
		}
		if n.fs.chunker == nil {
			n.fs.chunker = chunker.New(rd, n.fs.repo.Config().ChunkerPolynomial)
		} else {
			n.fs.chunker.Reset(rd, n.fs.repo.Config().ChunkerPolynomial)
		}
		blobs := restic.IDs{}
		for {
			chunk, err := n.fs.chunker.Next(n.fs.buf)
			if err == io.EOF {
				break
			} else if err != nil {
				return err
			}
			n.Node.Size += uint64(chunk.Length)

			id := restic.Hash(chunk.Data)
			if !n.fs.repo.Index().Has(restic.BlobHandle{ID: id, Type: restic.DataBlob}) {
				_, _, _, err := n.fs.repo.SaveBlob(n.fs.ctx, restic.DataBlob, chunk.Data, id, true)
				if err != nil {
					return err
				}

			}

			blobs = append(blobs, id)
		}
		n.Node.Content = blobs
		// We need to switch back to the read-only backing, but the node data
		// isn't yet fully committed to restic yet. When the full commit
		// finishes, the next call to open will open the file read-only.
		// XXX - we've invalidated the backing so all open handles are now
		// invalid and will segfault.
		n.SetBacking(nil)
		return nil
	case "dir":
		if n.subtree == nil {
			// Dir was never opened
			if n.Node.Subtree == nil {
				panic("no data for subtree")
			}
			return nil
		}
		id, err := n.subtree.Commit()
		if err == nil {
			n.Node.Subtree = &id
		}
		return err
	default:
		// Modifications to these node types are not supported, so there's
		// nothing to commit.
		return nil
	}
}

func (n *resticNode) makeWritable() error {
	tempfile, err := n.fs.Temporary.TempFile("", n.Node.Name)
	if err != nil {
		return err
	}
	source := n.Backing().(*resticFile)
	_, err = io.Copy(tempfile, source)
	if err != nil {
		return err
	}
	tempfile.Seek(0, io.SeekStart)
	n.SetBacking(tempfile)
	n.markDirty()
	err = source.Close()
	return err
}

func (n *resticNode) markDirty() {
	n.Node.Content = nil
	if n.parent != nil {
		n.parent.markDirty()
	}
}
