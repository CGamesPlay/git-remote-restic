#!/bin/bash
set -ueo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

banner() {
    { set +x; } 2>/dev/null
    echo ""
    echo "**********************************************************************"
    echo "$@"
    echo "**********************************************************************"
    set -x
}

export RESTIC_PASSWORD=password
export GIT_CONFIG_GLOBAL=/dev/null
export GIT_CONFIG_SYSTEM=/dev/null
export GIT_AUTHOR_NAME=git-restic-remote
export GIT_AUTHOR_EMAIL=nobody@example.com
export GIT_AUTHOR_DATE=2006-01-02T15:04:05-0700
export GIT_COMMITTER_NAME=git-restic-remote
export GIT_COMMITTER_EMAIL=nobody@example.com
export GIT_COMMITTER_DATE=2006-01-02T15:04:05-0700

if [ -z "${GIT_REMOTE_RESTIC_UNDER_TEST:-}" ]; then
    echo "\$GIT_REMOTE_RESTIC_UNDER_TEST not set; must identify a specific \`git-remote-restic\` binary by absolute path" >&2
    exit 1
fi
# Confirm that `git-remote-restic` resolves to the binary that was just built at the outset of `make test`.
# (`git-remote-restic` must actually be on the $PATH, and not just callable via $GIT_REMOTE_RESTIC_UNDER_TEST,
# since the $PATH is how git finds our remote helper for the `restic::` transport.)
if [ "$(command -v git-remote-restic)" != "$GIT_REMOTE_RESTIC_UNDER_TEST" ]; then
    echo "\$PATH resolves git-remote-restic to $(command -v git-remote-restic), not $GIT_REMOTE_RESTIC_UNDER_TEST" >&2
    exit 1
fi

rm -rf restic workdir
mkdir restic
tar xzf restic.tar.gz
git init --initial-branch=master workdir
cd workdir

echo "Test versions:"
restic version
git-remote-restic --version
echo ""

set -x
banner "Test that cloning from restic works"
git remote add origin restic::local:../restic
git fetch origin
git checkout origin/master -B master

banner "Test that pushing revisions to restic works"
echo 'Updated content' > README.md
git add .
git commit -m 'New content'
[ "$(git show --oneline HEAD | head -1)" == 'fad9cc3 New content' ]
git push origin master

banner "Test that an empty restic repository can be pushed to"
rm -rf ../restic
restic init -r ../restic
git push origin master

banner "Test that the restic repository works as a bare git repository"
cd ..
rm -rf workdir
restic restore -r restic latest --target workdir
cd workdir
[ "$(git show --oneline HEAD | head -1)" == 'fad9cc3 New content' ]

# Clean up
cd ..
rm -rf workdir restic
echo "All tests passed"
