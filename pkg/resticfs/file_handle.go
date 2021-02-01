package resticfs

import (
	"io"
	"os"

	"github.com/go-git/go-billy"
)

type fileHandle struct {
	n        *resticNode
	name     string
	flag     int
	isLocked bool
	isClosed bool
	position int64
}

var _ billy.File = (*fileHandle)(nil)

func newFileHandle(n *resticNode, name string, flag int) (*fileHandle, error) {
	f := &fileHandle{
		n:    n,
		name: name,
		flag: flag,
	}
	if flag&os.O_TRUNC != 0 {
		if err := f.Truncate(0); err != nil {
			return nil, err
		}
	}
	return f, nil
}

func (f *fileHandle) Name() string {
	return f.name
}

func (f *fileHandle) Lock() error {
	f.n.flock.Lock()
	f.isLocked = true
	return nil
}

func (f *fileHandle) Unlock() error {
	f.n.flock.Unlock()
	f.isLocked = false
	return nil
}

func (f *fileHandle) Truncate(size int64) error {
	backing := f.n.Backing()
	err := backing.Truncate(size)
	return err
}

func (f *fileHandle) Close() error {
	if f.isClosed {
		return os.ErrClosed
	}
	if f.isLocked {
		if err := f.Unlock(); err != nil {
			return err
		}
	}
	f.isClosed = true
	if f.flag&oWRITEABLE != 0 {
		f.n.openWriters--
	}
	return nil
}

func (f *fileHandle) Write(p []byte) (int, error) {
	if f.isClosed {
		return 0, os.ErrClosed
	}
	if f.flag&oWRITEABLE == 0 {
		return 0, os.ErrPermission
	}
	if f.flag&os.O_APPEND != 0 {
		// Need to atomically seek and write when this flag is specified.
		panic("O_APPEND not supported")
	}
	backing := f.n.Backing()
	n, err := backing.Write(p)
	return n, err
}

func (f *fileHandle) Read(b []byte) (int, error) {
	n, err := f.ReadAt(b, f.position)
	f.position += int64(n)
	if err == io.EOF && n != 0 {
		err = nil
	}
	return n, err
}

func (f *fileHandle) ReadAt(b []byte, pos int64) (int, error) {
	if f.isClosed {
		return 0, os.ErrClosed
	}
	backing := f.n.Backing()
	n, err := backing.ReadAt(b, pos)
	return n, err
}

func (f *fileHandle) Seek(offset int64, whence int) (int64, error) {
	if f.isClosed {
		return 0, os.ErrClosed
	}

	switch whence {
	case io.SeekCurrent:
		f.position += offset
	case io.SeekStart:
		f.position = offset
	case io.SeekEnd:
		f.position = int64(f.n.Node.Size) + offset
	}

	return f.position, nil
}
