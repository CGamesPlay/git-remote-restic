# git-remote-restic

This program provides a bridge between git and [restic](https://restic.net). This bridge allows git to push and pull from a git repository stored inside of a restic repository.

**Why?**

Git, on its own, does not provide a feature to use an untrusted remote. That means that if you push a repository to a remote, the remote has full visibility into the content and history of the repository.

Restic is a backup program specifically designed to store files securely in untrusted locations by using cryptography. By combining git and restic, users can store repositories securely on a variety of cloud providers using familiar tools: `git push` and `git pull`.

## Current status

Presently, `git-remote-restic` is functional and used by the author to keep secure backups of personal projects. For detailed information about future plans for the project, see [TODO.md](TODO.md).

This program is based on a fork of restic and may not be up-to-date with the latest restic features, however the restic project is conscientious of backwards compatibility. Here is [the compatibility note from the documentation](https://restic.net/#compatibility).

> Once version 1.0.0 is released, we guarantee backward compatibility of all repositories within one major version; as long as we do not increment the major version, data can be read and restored. We strive to be fully backward compatible to all prior versions.
>
> During initial development (versions prior to 1.0.0), maintainers and developers will do their utmost to keep backwards compatibility and stability, although there might be breaking changes without increasing the major version.

The latest version of this software is based on restic 0.16.4. The currently installed version can be checked by running `git-remote-restic --version`.

Bug reports are accepted.

## Usage

### Installation

1. [Install restic](https://restic.net/#installation) using the provided instructions.
2. Download a binary from [the releases page](https://github.com/CGamesPlay/git-remote-restic/releases) and place it in your PATH.
   Alternatively, use `go get github.com/CGamesPlay/git-remote-restic`

### Getting started

To use `git-remote-restic` with an existing git repository, first create a new restic repository to use, then push to it.

```bash
$ export RESTIC_REPOSITORY=s3:s3.amazonaws.com/my.bucket.name/path/to/repository
$ restic init
$ git remote add restic restic::$RESTIC_REPOSITORY
$ git push restic
```

A restic repository compatible with `git-remote-restic` can contain only one git repository, therefore it's recommended to use a path prefix in the restic URL to allow one storage bucket to contain multiple restic repositories. For example, you may wish to use `s3:s3.amazonaws.com/my.bucket.name/git/$repo` to keep all of your repositories in one bucket.

### Cloning from restic

To use `git-remote-restic` with an existing restic repository, simply use `git clone` with the restic URL.

```bash
$ export RESTIC_REPOSITORY=s3:s3.amazonaws.com/my.bucket.name/path/to/repository
$ git clone restic::$RESTIC_REPOSITORY
```

### Storing the repository password

To avoid typing the repository password repeatedly, `git-remote-restic` provides several methods to store it.

- If the environment variable `RESTIC_PASSWORD` is present, it specifies the password directly.
- If the environment variable `RESTIC_PASSWORD_FILE` is present, it specifies a path to a file which contains the password.
- Otherwise, [git credential](https://git-scm.com/docs/gitcredentials) provides the password.

Users may be interested in [this guide from GitHub](https://docs.github.com/en/github/using-git/caching-your-github-credentials-in-git) on how to use the git credential system to store passwords. Note that `RESTIC_PASSWORD_COMMAND` from restic is not supported.

### Verifying the repository

To verify that a restic repository has a complete and consistent copy of the git repository, you can restore the snapshot and verify it using git.

```bash
$ restic restore latest --target repo.git
$ cd repo.git
$ git fsck --strict
```

Generally, restic is able to detect when a snapshot has been corrupted during the restore process, however by using `git fsck --strict` we can also verify that no problems have been introduced by `git-remote-restic`.

## Technical details

Any restic repository which contains a snapshot rooted to a bare git repository is usable with `git-remote-restic`. For example, the following is functionally identical to what `git-remote-restic` does when pushing to a repository:

```bash
$ git clone --bare . ../repo.git
$ cd ../repo.git
$ restic backup .
```

This means that all standard restic commands can be used to work with the restic repository, including snapshot forgetting, pruning, and other maintenance. Additionally, the following is functionally equivalent to what `git-remote-restic` does when cloning from a repository:

```bash
$ restic restore latest --target repo.git
$ git clone repo.git repo
```

### Limitations

**You can't push a SHA1 without storing it in a temporary branch.** The underlying git library used in this project requires that we operate in reverse: when pushing to a restic repository, we metaphorically "cd" into the restic repository and then "fetch" the requested refs from the local one. Because of this behavior, it's not valid to push a SHA1 directly (because it's not valid to fetch a SHA1 directly). If you need to do this, you have to create a temporary branch, push, then delete the temporary branch.

## Prior art

There are other projects which fill a similar niche to `git-remote-restic`. Here are some of them, and the differences to `git-remote-restic`.

- GPG-based. `git-gpg`, `git-remote-gcrypt` (and likely others) rely on GPG, which greatly increases the installation and usage complexity. With `git-remote-restic`, two binaries and a password are the only requirements.
- Keybase. `git-remote-keybase` provides the same features as `git-remote-restic`, but the actual storage provider (Keybase's kbfs) is proprietary. With `git-remote-restic`, data can be hosted on a number of cloud storage providers, or [self-hosted](https://github.com/restic/rest-server).
