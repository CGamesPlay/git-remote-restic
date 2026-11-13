#!/usr/bin/env bash
#
# Populate ./restic using the restic.git version pinned in ./restic.config,
# then rename internal/ to lib/, since Go otherwise prevents us from importing those packages.
# Finally, copy restic's CLI setup code into our own `package main`, since Go cannot import restic's `package main`.
#
# (Note that go.mod redirects github.com/restic/restic dependencies to ./restic.)
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

url="$(git config -f restic.config restic.url)"
tag="$(git config -f restic.config restic.tag)"
commit="$(git config -f restic.config restic.commit)"

# Idempotence.
marker=restic/.git-remote-restic--restic-commit
if [[ -f $marker && "$(cat "$marker")" == "$commit" ]]; then
	exit 0
fi

staging="$(mktemp -d restic.staging.XXXX)"
trap 'rm -rf "$staging"' EXIT

# Fail spectacularly if the commit at the pinned tag does not match the pinned SHA-1.
# Use `git init` and `git fetch "refs/tags/$tag"` since a direct `git clone --branch "$tag"` outputs a warning.
git init -q --bare "$staging/restic.git"
git -C "$staging/restic.git" fetch -q --depth=1 "$url" "refs/tags/$tag"
actual_commit="$(git -C "$staging/restic.git" rev-parse 'FETCH_HEAD^{commit}')"
if [[ $actual_commit != "$commit" ]]; then
	echo "restic.git at url $url, tag $tag resolves to commit $actual_commit and not the expected commit $commit" >&2
	exit 1
fi

mkdir "$staging/restic"
git -C "$staging/restic.git" archive "$commit" | tar -x -C "$staging/restic"

mv "$staging/restic/internal" "$staging/restic/lib"
# Temporarily create, and then delete, `*.original` backup files,
# since BSD sed and GNU sed require mutually incompatible syntax (`-i ''` and `-i`, respectively)
# for any in-place editing sans backup.
find "$staging/restic" -name '*.go' -exec \
	sed -i.original 's|"github\.com/restic/restic/internal|"github.com/restic/restic/lib|g' {} +
find "$staging/restic" -name '*.go.original' -delete

echo "$commit" >"$staging/$marker"
rm -rf restic
mv "$staging/restic" restic

# This should only dirty our git repo if we are in the midst of changing the pinned restic.git version.
cp restic/cmd/restic/global.go cmd/git-remote-restic/
