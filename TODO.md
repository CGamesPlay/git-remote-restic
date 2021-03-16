# TODO

This document lists the major items in `git-remote-restic` which still need to be done.

**Incomplete features:**

- More automated tests are needed. Presently there is a small suite of end-to-end tests that verify that cloning, pushing, and restoration work properly. Manual testing has been performed to confirm that the software works on a variety of repositories.
- Restic is not optimized for repositories with hundreds or thousands of snapshots. The program should automatically remove old snapshots with some user configurable parameters. Note that in `git-remote-restic`, each snapshot has the full repository history (modulo history rewriting), so old snapshots are generally redundant anyways. 
- Git repositories which have many branches being created and removed do occasionally require maintenance in the form of `git gc`. Some thought is required on how that process should work in `git-remote-restic`, however it's lower priority since the target use case is archival rather than active development.
- Multi-user pushes.

## Algorithm for multi-user pushes

Presently, `git-remote-restic` acquires an exclusive lock on the repository for the duration of the push process. This is safe, but means that only a single user can be pushing at a time, even for pushes to different branches. In the future, an alternative process can be used to enable parallel pushes:

- Acquire an inclusive lock at the start, instead of an exclusive one.
- Ensure that the VFS avoids re-uploading an object if it's already in the repository.
- Before creating a snapshot, acquire an exclusive lock on the repository.
- Find the new latest snapshot and, if necessary, redo the push. This should be faster since the underlying objects will generally already be written to the repository.
- Create a new snapshot, unlock the repository.