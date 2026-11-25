PKG=github.com/CGamesPlay/git-remote-restic
RESTIC_VERSION = $(patsubst v%,%,$(shell git config -f restic.config restic.tag))
GOFLAGS_debug = -ldflags '-X "main.Version=$(shell git rev-parse --short HEAD; [ -z "$$(git status --porcelain --untracked-files=no)" ] || echo 'with uncommitted changes')" -X "main.ResticVersion=$(RESTIC_VERSION)"'
GOFLAGS_release = -ldflags '-s -w -extldflags "-static" -X "main.Version=$(shell cat VERSION)" -X "main.ResticVersion=$(RESTIC_VERSION)"'
OSARCHS = darwin/amd64 darwin/arm64 linux/amd64 linux/arm64
TEST_BIN_DIR = $(CURDIR)/bin/test

.PHONY: install
install: restic/.git-remote-restic--restic-commit
	go install $(GOFLAGS_debug) $(PKG)/...

restic/.git-remote-restic--restic-commit: restic.config scripts/fetch-restic.sh
	rm -f $@
	./scripts/fetch-restic.sh

.PHONY: test
test: restic/.git-remote-restic--restic-commit
	go build $(GOFLAGS_debug) -o $(TEST_BIN_DIR)/git-remote-restic $(PKG)/cmd/git-remote-restic
	go test $(PKG)/...
	PATH="$(TEST_BIN_DIR):$$PATH" GIT_REMOTE_RESTIC_UNDER_TEST="$(TEST_BIN_DIR)/git-remote-restic" ./fixtures/test.sh

.PHONY: bins
bins: restic/.git-remote-restic--restic-commit
	go install github.com/mitchellh/gox@latest
	gox -osarch "$(OSARCHS)" -output "bin/{{.OS}}_{{.Arch}}/git-remote-restic" $(GOFLAGS_release) $(PKG)/cmd/git-remote-restic

.PHONY: release
release: test $(patsubst %,bin/%.tar.gz,$(subst /,_,$(OSARCHS)))

define ruletemp
$(patsubst %,bin/%.tar.gz,$(subst /,_,$(1))): $(patsubst %,bin/%/git-remote-restic,$(subst /,_,$(1)))
	tar -czf $$@ -C $$(dir $$^) $$(notdir $$^)
$(patsubst %,bin/%/git-remote-restic,$(subst /,_,$(1))): bins

endef

$(foreach osarch,$(OSARCHS),$(eval $(call ruletemp, $(osarch))))
