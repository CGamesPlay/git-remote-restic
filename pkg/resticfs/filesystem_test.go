package resticfs

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/restic/restic/lib/backend"
	"github.com/restic/restic/lib/backend/local"
	"github.com/restic/restic/lib/data"
	"github.com/restic/restic/lib/repository"
	"github.com/restic/restic/lib/restic"
	"github.com/stretchr/testify/require"
)

var (
	testCtx = context.Background()
)

const (
	basicRepoURL           = "local:../../fixtures/basic"
	legacyUnsortedRepoPath = "../../fixtures/legacy-unsorted"
	fixtureRepoPassword    = "password"
)

// openTestRepo creates a test repository in memory and returns a Filesystem
// pointing to it.
func openTestRepo(t *testing.T) *Filesystem {
	t.Helper()

	repo := repository.TestRepository(t)

	fs, err := New(testCtx, repo, nil)
	if err != nil {
		panic(err)
	}
	return fs
}

// openBasicRepo loads the basic restic repo and returns it and the latest
// snapshot ID.
func openBasicRepo(t *testing.T) *Filesystem {
	t.Helper()

	return openFixtureRepo(t, basicRepoURL)
}

// openLegacyUnsortedRepo opens ./fixtures/legacy-unsorted,
// which contains trees with misordered nodes.
func openLegacyUnsortedRepo(t *testing.T) *Filesystem {
	t.Helper()

	repoPath := t.TempDir()
	require.NoError(t, os.CopyFS(repoPath, os.DirFS(legacyUnsortedRepoPath)))
	return openFixtureRepo(t, "local:"+repoPath)
}

// openFixtureRepo opens the latest snapshot of the restic repository at repoURL.
func openFixtureRepo(t *testing.T, repoURL string) *Filesystem {
	t.Helper()

	var err error
	config, err := local.ParseConfig(repoURL)
	if err != nil {
		panic(err)
	}
	var be backend.Backend
	if be, err = local.Open(testCtx, *config, t.Logf); err != nil {
		panic(err)
	}
	repo, err := repository.New(be, repository.Options{
		Compression: repository.CompressionOff,
		PackSize:    0,
	})
	if err != nil {
		panic(err)
	}
	if err = repo.SearchKey(testCtx, fixtureRepoPassword, 0, ""); err != nil {
		panic(err)
	}

	if err = repo.LoadIndex(testCtx, nil); err != nil {
		panic(err)
	}

	f := data.SnapshotFilter{}
	sn, _, err := f.FindLatest(testCtx, repo, repo, "latest")
	if err != nil {
		panic(err)
	}

	fs, err := New(testCtx, repo, sn.ID())
	if err != nil {
		panic(err)
	}
	return fs
}

func writeTestFile(t *testing.T, fs *Filesystem, path string) {
	t.Helper()

	file, err := fs.Create(path)
	require.NoError(t, err)
	_, err = file.Write([]byte("content of " + path + "\n"))
	require.NoError(t, err)
	require.NoError(t, file.Close())
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

func MakeNodeInfo(node data.Node) NodeInfo {
	return NodeInfo{&resticNode{Node: node}}
}

func requireTreeNodeNames(t *testing.T, fs *Filesystem, treeID restic.ID, expectedNodeNames []string) {
	t.Helper()

	nodes, err := data.LoadTree(testCtx, fs.repo, treeID)
	require.NoError(t, err)

	var actualNodeNames []string
	for item := range nodes {
		require.NoError(t, item.Error)
		actualNodeNames = append(actualNodeNames, item.Node.Name)
	}
	require.Equal(t, expectedNodeNames, actualNodeNames)
}

// loadDirectoryNodeNames maps
// the relative path of each (sub)directory within rootTreeID (including "" for the root directory itself)
// to that (sub)tree's node names ordered exactly as they are stored.
func loadDirectoryNodeNames(t *testing.T, fs *Filesystem, rootTreeID restic.ID) map[string][]string {
	t.Helper()

	directoryNodeNames := map[string][]string{}

	var loadDirectory func(directoryPath string, treeID restic.ID)
	loadDirectory = func(directoryPath string, treeID restic.ID) {
		nodes, err := data.LoadTree(testCtx, fs.repo, treeID)
		require.NoError(t, err, directoryPath)

		nodeNames := []string{}
		for item := range nodes {
			require.NoError(t, item.Error, directoryPath)
			nodeNames = append(nodeNames, item.Node.Name)
			if item.Node.Type == "dir" {
				loadDirectory(path.Join(directoryPath, item.Node.Name), *item.Node.Subtree)
			}
		}
		directoryNodeNames[directoryPath] = nodeNames
	}

	loadDirectory("", rootTreeID)
	return directoryNodeNames
}

func TestReadDir(t *testing.T) {
	fs := openBasicRepo(t)

	expectedRoot := []os.FileInfo{
		MakeNodeInfo(data.Node{Name: "README.md", Size: 116, Mode: os.FileMode(0644)}),
		MakeNodeInfo(data.Node{Name: "images", Size: 0, Mode: os.ModeDir | os.FileMode(0755)}),
	}

	expectedImages := []os.FileInfo{
		MakeNodeInfo(data.Node{Name: "IMG_8646.jpeg", Size: 1635171, Mode: os.FileMode(0644)}),
	}

	items, err := fs.ReadDir("")
	require.NoError(t, err)
	RequireFileInfoEqual(t, expectedRoot, items)
	items, err = fs.ReadDir("images")
	require.NoError(t, err)
	RequireFileInfoEqual(t, expectedImages, items)
}

func TestStat(t *testing.T) {
	fs := openBasicRepo(t)

	expected := MakeNodeInfo(data.Node{Name: "IMG_8646.jpeg", Size: 1635171, Mode: os.FileMode(0644)})
	fi, err := fs.Stat("/images/IMG_8646.jpeg")
	require.NoError(t, err)
	RequireFileInfoEqual(t, []os.FileInfo{expected}, []os.FileInfo{fi})
}

func TestReadFile(t *testing.T) {
	fs := openBasicRepo(t)
	expected := "# Sample Directory\n\nThis directory has some files but isn't a git repository. It's for testing the raw vfs methods.\n"
	file, err := fs.Open("README.md")
	require.NoError(t, err)
	actual, err := ioutil.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, expected, string(actual))
}

func TestWriteFile(t *testing.T) {
	fs := openTestRepo(t)
	fs.StartNewSnapshot()

	file, err := fs.Create("file-1")
	require.NoError(t, err)
	_, err = file.Write([]byte("content of file-1\n"))
	require.NoError(t, err)
	err = file.Close()
	require.NoError(t, err)

	id, err := fs.CommitSnapshot("/tmp", []string{})
	require.NoError(t, err)
	require.NotEmpty(t, id)
}

func TestMkdirAll(t *testing.T) {
	fs := openTestRepo(t)
	fs.StartNewSnapshot()

	err := fs.MkdirAll("foo/bar", 0777)
	require.NoError(t, err)
	file, err := fs.Create("foo/bar/file-1")
	require.NoError(t, err)
	_, err = file.Write([]byte("content of file-1\n"))
	require.NoError(t, err)
	err = file.Close()
	require.NoError(t, err)
	err = fs.MkdirAll("foo/bar/file-1", 0777)
	require.Equal(t, err, ErrNotDirectory)

	id, err := fs.CommitSnapshot("/tmp", []string{})
	require.NoError(t, err)
	require.NotEmpty(t, id)
}

// restic expects trees' nodes to be ordered by name
// and [now enforces this at write time](https://github.com/restic/restic/commit/350f29d921cceff60bc0de06b9df0cec33182712).
func TestCommitSnapshot_SortsTreeNodes(t *testing.T) {
	fs := openTestRepo(t)
	fs.StartNewSnapshot()

	// Nodes written _out of order_, by e.g. go-git.
	writeTestFile(t, fs, "b")
	writeTestFile(t, fs, "a")
	writeTestFile(t, fs, "d")
	require.NoError(t, fs.MkdirAll("c", 0777))
	writeTestFile(t, fs, "c/z")
	writeTestFile(t, fs, "c/y")

	snapshotID, err := fs.CommitSnapshot("/tmp", []string{})
	require.NoError(t, err)
	snapshot, err := data.LoadSnapshot(testCtx, fs.repo, snapshotID)
	require.NoError(t, err)
	requireTreeNodeNames(t, fs, *snapshot.Tree, []string{"a", "b", "c", "d"})

	subtreeID, err := data.FindTreeDirectory(testCtx, fs.repo, snapshot.Tree, "c")
	require.NoError(t, err)
	requireTreeNodeNames(t, fs, *subtreeID, []string{"y", "z"})
}

// In legacy snapshots from prior versions of git-remote-restic,
// a tree's nodes reflect the order in which nodes were created by go-git.
// Confirm that _we_ are still able to read such unsorted trees
// (in contrast to e.g. single-file `restic dump ...` commands).
func TestOpen_LegacyUnsortedTrees(t *testing.T) {
	fs := openLegacyUnsortedRepo(t)
	requireTreeNodeNames(t, fs, *fs.root.ID, []string{"objects", "refs", "HEAD", "config"})

	expectedContents := map[string]string{
		"HEAD":               "ref: refs/heads/master\n",
		"refs/heads/master":  "0b2e1c9886ffc4315f2b5650b9c58a5e2b87780b\n",
		"refs/heads/develop": "064b5f571034df07302e224eab5fb61af5ef2f5a\n",
	}
	for path, expectedContent := range expectedContents {
		file, err := fs.Open(path)
		require.NoError(t, err, path)
		actualContent, err := io.ReadAll(file)
		require.NoError(t, err, path)
		require.Equal(t, expectedContent, string(actualContent), path)
	}
}

// Modifying a legacy snapshot sorts the nodes in every tree,
// not just those in modified trees.
func TestCommitSnapshot_SortsAllLegacyTrees(t *testing.T) {
	fs := openLegacyUnsortedRepo(t)

	const firstPack = "pack-167bd03ac705c335a04c566c698dbe7ce407b631"
	const secondPack = "pack-4088f24ace9fd5e8816a32c8be76dc46364ad2e5"
	require.Equal(t, map[string][]string{
		"":             {"objects", "refs", "HEAD", "config"},
		"objects":      {"info", "pack"},
		"objects/info": {},
		"objects/pack": {firstPack + ".pack", firstPack + ".idx", secondPack + ".pack", secondPack + ".idx"},
		"refs":         {"heads", "tags"},
		"refs/heads":   {"master", "develop"},
		"refs/tags":    {},
	}, loadDirectoryNodeNames(t, fs, *fs.root.ID))

	// Modify only refs/heads.
	fs.StartNewSnapshot()
	writeTestFile(t, fs, "refs/heads/feature")

	// Require _all_ directories' nodes to be sorted.
	snapshotID, err := fs.CommitSnapshot("/tmp", []string{})
	require.NoError(t, err)
	snapshot, err := data.LoadSnapshot(testCtx, fs.repo, snapshotID)
	require.NoError(t, err)
	require.Equal(t, map[string][]string{
		"":             {"HEAD", "config", "objects", "refs"}, // ancestor of the modified directory, and now sorted
		"objects":      {"info", "pack"},
		"objects/info": {},
		"objects/pack": {firstPack + ".idx", firstPack + ".pack", secondPack + ".idx", secondPack + ".pack"}, // not modified, but sorted anyway
		"refs":         {"heads", "tags"},
		"refs/heads":   {"develop", "feature", "master"}, // directly modified, and now sorted
		"refs/tags":    {},
	}, loadDirectoryNodeNames(t, fs, *snapshot.Tree))
}
