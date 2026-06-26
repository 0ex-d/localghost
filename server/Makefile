# LocalGhost server build. One module (github.com/LocalGhostDao/localghost/server), many binaries
# under cmd/, all sharing the library packages under internal/ (compiler-enforced privacy , they
# cannot be imported outside this module).
#
# Each cmd/<name> dir is named EXACTLY for the binary it produces (cmd/ghost.secd -> ghost.secd), so
# binaries land in bin/ with the right names and no remapping. Daemons use the dotted ghost. prefix
# (matching systemd units + the GhostDaemons list); operator tools use ghost-; benchmarks keep their
# own names. Some daemon dirs are still STUBS (README only, no .go yet) , the build auto-skips any
# cmd/ dir that has no Go files, so `make all` works as you flesh the daemons out.

GO      ?= go
BINDIR  := bin
TAGS    ?=

# Discover every cmd/<name> that actually contains Go source (skips README-only stubs).
CMDS := $(sort $(dir $(wildcard cmd/*/*.go)))

# all builds every buildable cmd into bin/, each named for its dir.
.PHONY: all
all:
	@for d in $(CMDS); do \
		echo "building $$(basename $$d)"; \
		$(GO) build -tags '$(TAGS)' -o $(BINDIR)/ ./$$d || exit 1; \
	done

# box: the real hardware build , links the TPM/dm-crypt/datastore backend (needs go-tpm). -tags tpm
# applies to all; the tools/benchmarks ignore it harmlessly.
.PHONY: box
box:
	@$(MAKE) all TAGS=tpm

# Individual targets, handy during development.
.PHONY: secd
secd:
	$(GO) build -tags '$(TAGS)' -o $(BINDIR)/ ./cmd/ghost.secd

.PHONY: setup
setup:
	$(GO) build -o $(BINDIR)/ ./cmd/ghost-setup

.PHONY: ctl
ctl:
	$(GO) build -o $(BINDIR)/ ./cmd/ghost-ctl

# build a single daemon by name, e.g. `make daemon NAME=framed`
.PHONY: daemon
daemon:
	$(GO) build -tags '$(TAGS)' -o $(BINDIR)/ ./cmd/ghost.$(NAME)

.PHONY: test
test:
	$(GO) test ./...

.PHONY: clean
clean:
	rm -rf $(BINDIR)