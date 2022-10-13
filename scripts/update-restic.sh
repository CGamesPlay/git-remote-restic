#!/usr/bin/bash
set -ueo pipefail

tag=$1

cd restic
git fetch 'git@github.com:restic/restic.git' "$tag"
git checkout FETCH_HEAD
mv internal lib
find . -name '*.go' -exec sed -i -e 's/"github.com\/restic\/restic\/internal\b/"github.com\/restic\/restic\/lib/' {} \;
git add .
git commit -m "Rename internal to lib"
git tag "$tag-gitremote"
git push 'git@github.com:CGamesPlay/restic.git' "$tag-gitremote"
