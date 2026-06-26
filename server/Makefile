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

# all builds every binary under cmd/ into bin/, each named for its dir. The TPM-tagged build (box)
# is separate because the daemons need -tags tpm to link the real hardware backend.
.PHONY: all
all:
	$(GO) build -o $(BINDIR)/ ./cmd/...

# box: the real hardware build , links the TPM/dm-crypt/datastore backend (needs go-tpm in the
# module). -tags tpm applies to everything; the tools ignore it harmlessly.
.PHONY: box
box:
	$(GO) build -tags tpm -o $(BINDIR)/ ./cmd/...

# Individual targets, handy during development. `go build -o bin/` drops the binary named for the dir.
.PHONY: secd
secd:
	$(GO) build -tags '$(TAGS)' -o $(BINDIR)/ ./cmd/ghost.secd

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
