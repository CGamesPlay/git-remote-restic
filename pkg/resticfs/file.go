package resticfs

import (
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/go-git/go-billy"
	"github.com/restic/restic/lib/restic"
)

type resticFile struct {
	fs   *Filesystem
	node *restic.Node
	// cumsize holds the cumulatoive size of blobs[:i]
	cumsize  []uint64
	isClosed bool
	position int64
}

var _ billy.File = (*resticFile)(nil)

func newFile(fs *Filesystem, node *restic.Node) (*resticFile, error) {
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
	return os.ErrPermission
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
	return 0, os.ErrPermission
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
