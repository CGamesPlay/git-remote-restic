# `legacy-unsorted`

This is a "legacy" restic repository created by git-remote-restic v1.1.18,
in which each tree's nodes array reflects the order in which nodes were created by go-git.
It contains two snapshots of a bare git repository,
the first having a `master` branch alone and the second having an additional `develop` branch.
Both snapshots' root and `objects/pack` trees are misordered, as is the second snapshot's `refs/heads`;
any such tree's nodes must therefore be sorted if one wishes to resave the tree using restic v0.19.

The creation process:

1. `restic init` using restic v0.18.0, with password `"password"`.
2. `git push origin master` and `git push origin develop` using git-remote-restic v1.1.18,
   with the same git author/committer/dates as in `./fixtures/test.sh`,
   but with anonymized `uid`, `gid`, `userName`, `groupName`, `hostname`, `snapshot.Username`, `snapshot.UID`, `snapshot.GID` (in `./pkg/resticfs/filesystem.go`)
   and anonymized `localGitPath` (in `./cmd/git-remote-restic/git.go`).
3. `restic key add --user ... --host ...` and `restic key remove ...` to replace the key created by `restic init` with an anonymized one.
