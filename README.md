# git-remote-restic

This is a prototype version of a git remote that stores data in a restic repository. The existing code is built on the git-remote-keybase and does some hacks to ease the integration with go-git. This is because go-git can act as a client for the git wire protocol, but [nothing exists to allow it to act as a server](https://github.com/go-git/go-git/issues/152).

## Current status

`cmd/git-remote-restic`

- Document how to use `git credential` to store the repo password.

## Plan for pushing to restic

After looking through [git-remote-keybase](https://github.com/keybase/client/blob/cd76ccb97183c2be78b869fab9aed4b6f5b11086/go/kbfs/kbfsgit/runner.go), it looks like I'm overthinking things. What this program does is basically provide a VFS on kbfs, and then just push using go-git to a bare repository there. I could do a similar thing with restic, the basic process would look like this:

- For a prototype, acquire an exclusive lock on the repository at this point.
- Use go-git to push to a VFS that I write. Implement [go-billy](https://pkg.go.dev/github.com/go-git/go-billy/v5). Use [restic's fuse fs](https://github.com/restic/restic/blob/aa0faa8c7d7800b6ba7b11164fa2d3683f7f78aa/internal/fuse/dir.go#L65) as an example.
- The VFS loads the latest snapshot to populate itself.
- When the VFS writes a file, push it into the restic repository, but store the nodes in memory.
- Create a new snapshot with the in-memory tree.

Later, to implement multi-user features, the steps change.

- Acquire an inclusive lock at the start, instead of an exclusive one.
- Ensure that the VFS avoids re-uploading an object if it's already in the repository.
- Before creating a snapshot, acquire an exclusive lock on the repository.
- Find the new latest snapshot and recreate the base VFS layer from this snapshot.
- Detect non-fast-forwards and fail the push if detected.
- Create a new snapshot with the in-memory tree.
- Otherwise, create a new snapshot with the latest tree.

## Reference

Keybase code: [libgit](https://github.com/keybase/client/blob/cac9573e33f472fcb1417c1e6a899bfbba36405c/go/kbfs/libgit/), also check `kbfsgit` for the go-git application code.