#!/bin/bash
set -ueio pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
export RESTIC_PASSWORD=password
export GIT_AUTHOR_NAME=git-restic-remote
export GIT_AUTHOR_EMAIL=nobody@example.com
export GIT_COMMITTER_NAME=git-restic-remote
export GIT_COMMITTER_EMAIL=nobody@example.com
mkdir restic
tar xzf restic.tar.gz
git init workdir
cd workdir

# Test that cloning from restic works
git remote add origin restic::local:../restic
git fetch origin
git checkout origin/master -B master

# Test that pushing revisions to restic works
echo 'Updated content' > README.md
git add .
git commit -m "New content"
git push origin master

# Test that the restic repository works as a bare git repository
cd ..
rm -rf workdir
restic restore -r restic latest --target workdir
cd workdir
git show --oneline HEAD | head -1 | grep -q 'Updated content'

# Clean up
cd ..
rm -rf workdir restic
