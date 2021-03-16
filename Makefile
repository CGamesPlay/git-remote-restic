PKG=github.com/CGamesPlay/git-remote-restic
SOURCES = $(shell find . -name '*.go') go.mod go.sum
RESTIC_VERSION = $(shell cat restic/VERSION)
GOFLAGS_debug = -ldflags '-X "main.Version=$(shell git rev-parse --short HEAD; [ -z "$$(git status --porcelain --untracked-files=no)" ] || echo 'with uncommitted changes')" -X "main.ResticVersion=$(RESTIC_VERSION)"'
GOFLAGS_release = -ldflags '-s -w -extldflags "-static" -X "main.Version=$(shell cat VERSION)" -X "main.ResticVersion=$(RESTIC_VERSION)"'

.PHONY: install
install:
	go install $(GOFLAGS_debug) $(PKG)/...

.PHONY: test
test: install
	go test $(PKG)/...
	./fixtures/test.sh

.PHONY: release
release: bin/darwin_amd64.tar.gz bin/linux_amd64.tar.gz

.PHONY: bin/darwin_amd64/git-remote-restic
bin/darwin_amd64/git-remote-restic:
	mkdir -p $(dir $@)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o $@ $(GOFLAGS_release) $(PKG)/cmd/git-remote-restic

.PHONY: bin/linux_amd64/git-remote-restic
bin/linux_amd64/git-remote-restic:
	mkdir -p $(dir $@)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $@ $(GOFLAGS_release) $(PKG)/cmd/git-remote-restic

bin/%.tar.gz: bin/%/git-remote-restic
	tar -czf $@ -C $(dir $^) $(notdir $^)
