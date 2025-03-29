# Maintenance

## Upgrading the restic version

1. Use `./scripts/update-restic.sh pull vX.Y.Z` to update the submodule. It automatically pushes a modified tag to the custom restic fork on Github.
3. Use `go mod tidy` to fetch all the modules and remove the unused ones.
4. Use `./scripts/update-restic.sh update-cmd` to update the repository opening code.
   - This copies the top-level options configuration which is mostly unused, but is required for parsing the backend from the restic URL.
5. Use `make test` to verify everything still works.
   - Use `git log -pG <pattern>` to identify commits that changed APIs that are now broken.
6. Update the VERSION file to the new version and update the README to indicate the correct version of restic.
7. Make a commit.
8. Use `make release` to compile a new release.
9. Push everything to Github, and make a new release there.
