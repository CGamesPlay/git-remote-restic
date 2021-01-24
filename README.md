# git-remote-restic

This is a prototype version of a git remote that stores data in a restic repository.

Basic steps required to get this working:
- Destroy everything and write a hack to transfer an entire repository using restic and go-git.
- It's presently implemented as a fast-export processor, but this is a bad direction. In order to properly integrate with restic, it is necessary to build a restic VFS. This will be much more easily accomplished using go-git.
- There needs to be consideration on the "reflist" snapshot. It should be structured in such a way where it properly references all of the content in the repository, so that `restic prune` doesn't destroy it.
