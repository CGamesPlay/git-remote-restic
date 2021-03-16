#!/bin/bash
set -ueio pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

banner() {
    { set +x; } 2>/dev/null
    echo ""
    echo "$@"
    echo "==="
    set -x
}

export RESTIC_PASSWORD=password
export GIT_AUTHOR_NAME=git-restic-remote
export GIT_AUTHOR_EMAIL=nobody@example.com
export GIT_AUTHOR_DATE=2005-04-08T22:13:13
export GIT_COMMITTER_NAME=git-restic-remote
export GIT_COMMITTER_EMAIL=nobody@example.com
export GIT_COMMITTER_DATE=2005-04-08T22:13:13
mkdir restic
tar xzf restic.tar.gz
git init workdir
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
[ "$(git show --oneline HEAD | head -1)" == '38fa337 New content' ]

# Clean up
cd ..
rm -rf workdir restic
echo "All tests passed"
