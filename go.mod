module github.com/CGamesPlay/git-remote-restic

go 1.13

replace github.com/restic/restic => ./restic

require (
	github.com/go-git/go-billy/v5 v5.0.0
	github.com/go-git/go-git/v5 v5.2.0
	github.com/pkg/errors v0.9.1
	github.com/restic/restic v0.0.0-00010101000000-000000000000
)
