# LocalGhost server build. One module (github.com/LocalGhostDao/localghost/server), many binaries
# under cmd/, all sharing the library packages under internal/ (internal/auth, internal/profile,
# internal/pair, internal/hw, internal/secd, ...). internal/ means they cannot be imported outside
# this module , compiler-enforced privacy.
#
# Each cmd/<name> dir is named EXACTLY for the binary it produces (cmd/ghost.secd -> ghost.secd),
# so `go build -o bin/ ./cmd/...` and `go install ./...` produce correctly-named binaries with no
# remapping. The front door is ghost.secd; ghost-setup and ghost-ctl are the operator tools; the
# backing daemons are cmd/ghost.<x> (ghost.voiced, ghost.framed, ...). Binary names match the
# systemd unit ExecStart and the GhostDaemons list.

GO      ?= go
BINDIR  := bin
TAGS    ?=

# all builds every binary under cmd/ into bin/, each named for its dir. There is ONE build now: the
# seal tier (TPM or software) is selected at RUNTIME from seal.env, not a build tag, so a box can
# change hardware without recompiling. go-tpm is always linked (pure Go, no device access at init).
.PHONY: all
all:
	$(GO) build -o $(BINDIR)/ ./cmd/...

# box is kept as an alias of all so existing muscle-memory / docs still work; there is no separate
# hardware build any more.
.PHONY: box
box: all

# Individual targets, handy during development.
.PHONY: secd
secd:
	$(GO) build -o $(BINDIR)/ ./cmd/ghost.secd

.PHONY: setup
setup:
	$(GO) build -o $(BINDIR)/ ./cmd/ghost-setup

.PHONY: ctl
ctl:
	$(GO) build -o $(BINDIR)/ ./cmd/ghost-ctl

.PHONY: test
test:
	$(GO) test ./...

.PHONY: clean
clean:
	rm -rf $(BINDIR)
