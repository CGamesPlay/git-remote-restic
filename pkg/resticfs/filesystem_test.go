package resticfs

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/restic/restic/lib/backend/local"
	"github.com/restic/restic/lib/repository"
	"github.com/restic/restic/lib/restic"
	"github.com/stretchr/testify/require"
)

var (
	testCtx = context.Background()
)

const (
	basicRepoURL      = "local:../../fixtures/basic"
	basicRepoPassword = "password"
)

// openBasicRepo loads the basic restic repo and returns it and the latest
// snapshot ID.
func openBasicRepo() *Filesystem {
	var err error
	config, err := local.ParseConfig(basicRepoURL)
	if err != nil {
		panic(err)
	}
	var be restic.Backend
	if be, err = local.Open(testCtx, config.(local.Config)); err != nil {
		panic(err)
	}
	repo := repository.New(be)
	if err = repo.SearchKey(testCtx, basicRepoPassword, 0, ""); err != nil {
		panic(err)
	}

	if err = repo.LoadIndex(testCtx); err != nil {
		panic(err)
	}

	id, err := restic.FindLatestSnapshot(testCtx, repo, nil, nil, nil)
	if err != nil {
		panic(err)
	}

	fs, err := New(testCtx, repo, &id)
	if err != nil {
		panic(err)
	}
	return fs
}

func formatFileInfo(info []os.FileInfo) string {
	res := &strings.Builder{}
	for _, fi := range info {

		fmt.Fprintf(res, " %v %10d %v\n", fi.Mode(), fi.Size(), fi.Name())
	}
	return res.String()
}

func RequireFileInfoEqual(t *testing.T, expected, actual []os.FileInfo) {
	require.NotNil(t, expected)
	require.NotNil(t, actual)
	eStr := formatFileInfo(expected)
	aStr := formatFileInfo(actual)
	require.Equal(t, eStr, aStr)
}

func TestReadDir(t *testing.T) {
	fs := openBasicRepo()

	expectedRoot := []os.FileInfo{
		NodeInfo{&restic.Node{Name: "README.md", Size: 116, Mode: os.FileMode(0644)}},
		NodeInfo{&restic.Node{Name: "images", Size: 0, Mode: os.ModeDir | os.FileMode(0755)}},
	}

	expectedImages := []os.FileInfo{
		NodeInfo{&restic.Node{Name: "IMG_8646.jpeg", Size: 1635171, Mode: os.FileMode(0644)}},
	}

	items, err := fs.ReadDir("")
	require.NoError(t, err)
	RequireFileInfoEqual(t, expectedRoot, items)
	items, err = fs.ReadDir("images")
	require.NoError(t, err)
	RequireFileInfoEqual(t, expectedImages, items)
}

func TestStat(t *testing.T) {
	fs := openBasicRepo()

	expected := NodeInfo{&restic.Node{Name: "IMG_8646.jpeg", Size: 1635171, Mode: os.FileMode(0644)}}
	fi, err := fs.Stat("/images/IMG_8646.jpeg")
	require.NoError(t, err)
	RequireFileInfoEqual(t, []os.FileInfo{expected}, []os.FileInfo{fi})
}

func TestReadFile(t *testing.T) {
	fs := openBasicRepo()
	expected := "# Sample Directory\n\nThis directory has some files but isn't a git repository. It's for testing the raw vfs methods.\n"
	file, err := fs.Open("README.md")
	require.NoError(t, err)
	actual, err := ioutil.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, expected, string(actual))
}
