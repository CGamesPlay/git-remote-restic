# Maintenance

## restic dependency

Go prevents us from importing restic's (or any module's) `internal/` packages.

`./scripts/fetch-restic.sh` therefore fetches the restic.git version pinned in `./restic.config`,
places it in an (untracked) `./restic` directory,
and therein renames `internal/` to `lib/` and rewrites imports accordingly.
It then copies restic's top-level CLI options configuration, `cmd/restic/global.go`, to `cmd/git-remote-restic/global.go`,
which is mostly unused but is required for parsing the backend from a restic URL.
`go.mod` uses a `replace` directive to redirect all our `github.com/restic/restic` imports to `./restic`.

`make` automatically runs `./scripts/fetch-restic.sh` whenever `./restic.config` changes.
`./restic.config` names a restic.git repository URL, the desired git tag, and the corresponding git commit's SHA-1 for verification.

## Upgrading the restic version

1. Update the `restic.tag` in `./restic.config`,
   and update the `restic.commit` to reflect `git ls-remote https://github.com/restic/restic.git "refs/tags/$tag^{}"`.
2. Run `./scripts/fetch-restic.sh` to update your untracked `./restic` and the repository opening code in `cmd/git-remote-restic/global.go`.
3. Use `go mod tidy` to fetch all the modules and remove the unused ones.
5. Use `make test` to verify everything still works.
   - Use `git log -pG <pattern>` in a clone of restic to identify commits that changed APIs that are now broken.
6. Update the VERSION file to the new version and update the README to indicate the correct version of restic.
7. Make a commit.
8. Use `make release` to compile a new release.
9. Push everything to Github, and make a new release there.
