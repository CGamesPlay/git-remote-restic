PKG=github.com/CGamesPlay/git-remote-restic
RESTIC_VERSION = $(shell cat restic/VERSION)
GOFLAGS_debug = -ldflags '-X "main.Version=$(shell git rev-parse --short HEAD; [ -z "$$(git status --porcelain --untracked-files=no)" ] || echo 'with uncommitted changes')" -X "main.ResticVersion=$(RESTIC_VERSION)"'
GOFLAGS_release = -ldflags '-s -w -extldflags "-static" -X "main.Version=$(shell cat VERSION)" -X "main.ResticVersion=$(RESTIC_VERSION)"'
OSARCHS = darwin/amd64 darwin/arm64 linux/amd64 linux/arm64

.PHONY: install
install:
	go install $(GOFLAGS_debug) $(PKG)/...

.PHONY: test
test: install
	go test $(PKG)/...
	./fixtures/test.sh

.PHONY: bins
bins:
	go install github.com/mitchellh/gox@latest
	gox -osarch "$(OSARCHS)" -output "bin/{{.OS}}_{{.Arch}}/git-remote-restic" $(GOFLAGS_release) $(PKG)/cmd/git-remote-restic

.PHONY: release
release: $(patsubst %,bin/%.tar.gz,$(subst /,_,$(OSARCHS)))

define ruletemp
$(patsubst %,bin/%.tar.gz,$(subst /,_,$(1))): $(patsubst %,bin/%/git-remote-restic,$(subst /,_,$(1)))
	tar -czf $$@ -C $$(dir $$^) $$(notdir $$^)
$(patsubst %,bin/%/git-remote-restic,$(subst /,_,$(1))): bins

endef

$(foreach osarch,$(OSARCHS),$(eval $(call ruletemp, $(osarch))))
