#!/bin/bash
set -xueo pipefail

tag=$1

git submodule update --init
cd restic
git fetch 'git@github.com:restic/restic.git' "$tag"
git checkout FETCH_HEAD
mv internal lib
find . -name '*.go' -exec sed -i -e 's/"github.com\/restic\/restic\/internal/"github.com\/restic\/restic\/lib/' {} \;
make # Just a test to ensure that the sed script worked.
git add .
git commit -m "Rename internal to lib"
git tag "$tag-gitremote"
git push 'git@github.com:CGamesPlay/restic.git' "$tag-gitremote"
