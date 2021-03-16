#!/bin/bash
set -ueio pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
export RESTIC_PASSWORD=password
export GIT_AUTHOR_NAME=git-restic-remote
export GIT_AUTHOR_EMAIL=nobody@example.com
export GIT_AUTHOR_DATE=2005-04-07T22:13:13
export GIT_COMMITTER_NAME=git-restic-remote
export GIT_COMMITTER_EMAIL=nobody@example.com
export GIT_COMMITTER_DATE=2005-04-07T22:13:13
mkdir restic
git init --bare git
git clone git workdir
cd workdir
echo 'Base revision' > README.md
git add .
git commit -m "Initial commit"
git push origin master
cd ..
rm -rf workdir
restic init -r restic
cd git
restic backup -r ../restic .
cd ..
tar czf restic.tar.gz restic
rm -rf git restic
