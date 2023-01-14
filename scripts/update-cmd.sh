#!/bin/bash
set -xueo pipefail

cp restic/cmd/restic/global.go cmd/git-remote-restic/restic.go
/opt/pkg/gnu/bin/patch --merge cmd/git-remote-restic/restic.go scripts/update-cmd.patch
