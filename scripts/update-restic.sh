#!/usr/bin/env bash

set -eu

# @cmd Script to update internal restic version
# @arg tag!  Tag to pull from restic
pull() {
	git submodule update --init
	cd restic
	git fetch 'git@github.com:restic/restic.git' "${argc_tag:?}"
	git checkout FETCH_HEAD
	mv internal lib
	find . -name '*.go' -exec sed -i -e 's/"github.com\/restic\/restic\/internal/"github.com\/restic\/restic\/lib/' {} \;
	make # Just a test to ensure that the sed script worked.
	git add .
	git commit -m "Rename internal to lib"
	git tag "${argc_tag:?}-gitremote"
	git push 'git@github.com:CGamesPlay/restic.git' "${argc_tag:?}-gitremote"
}

# @cmd Pull in the restic CLI configuration
update-cmd() {
	cp restic/cmd/restic/global.go cmd/git-remote-restic/restic.go
	gpatch --merge cmd/git-remote-restic/restic.go scripts/update-cmd.patch
}

if ! command -v argc >/dev/null; then
	echo "This command requires argc. Install from https://github.com/sigoden/argc" >&2
	exit 100
fi
eval "$(argc --argc-eval "$0" "$@")"
