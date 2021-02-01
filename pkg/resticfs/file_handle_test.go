package resticfs

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCopyOnWrite(t *testing.T) {
	fs := openBasicRepo()
	fs.StartNewSnapshot()

	h1, err := fs.Open("README.md")
	require.NoError(t, err)
	b1 := make([]byte, 20)
	_, err = h1.Read(b1)
	require.NoError(t, err)
	require.Equal(t, []byte("# Sample Directory\n\n"), b1)

	h2, err := fs.Create("README.md")
	require.NoError(t, err)

	_, err = h1.Read(b1)
	require.Equal(t, io.EOF, err)

	_, err = h2.Write([]byte("# Sample Directory\n\nBut with revised content.\n"))
	require.NoError(t, err)
	err = h2.Close()
	require.NoError(t, err)

	_, err = h1.ReadAt(b1, 20)
	require.NoError(t, err)
	require.Equal(t, []byte("But with revised con"), b1)
}
