PKG=github.com/CGamesPlay/git-remote-restic
SOURCES = $(wildcard *.go) go.mod go.sum
GOFLAGS = -ldflags '-s -w -extldflags "-static"'

.PHONY: install
install:
	go install $(PKG)/...

.PHONY: test
test: install
	go test $(PKG)/...

.PHONY: release
release: bin/darwin_amd64.tar.gz bin/linux_amd64.tar.gz

bin/darwin_amd64/git-remote-restic: $(SOURCES)
	mkdir -p $(dir $@)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o $@ $(GOFLAGS) $(PKG)/cmd/git-remote-restic

bin/linux_amd64/git-remote-restic: $(SOURCES)
	mkdir -p $(dir $@)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $@ $(GOFLAGS) $(PKG)/cmd/git-remote-restic

bin/%.tar.gz: bin/%/git-remote-restic
	tar -czf $@ -C $(dir $^) $(notdir $^)
