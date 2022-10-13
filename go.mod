module github.com/CGamesPlay/git-remote-restic

go 1.13

replace github.com/restic/restic => ./restic

require (
	github.com/go-git/go-billy v4.2.0+incompatible
	github.com/go-git/go-billy/v5 v5.0.0
	github.com/go-git/go-git/v5 v5.2.0
	github.com/hashicorp/golang-lru v0.5.4
	github.com/pkg/errors v0.9.1
	github.com/restic/chunker v0.4.0
	github.com/restic/restic v0.0.0-00010101000000-000000000000
	github.com/stretchr/testify v1.7.0
	golang.org/x/sync v0.0.0-20220819030929-7fc1605a5dde
	gopkg.in/src-d/go-billy.v4 v4.3.2 // indirect
)
