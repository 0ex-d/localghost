# LocalGhost server build. One module (github.com/LocalGhostDao/localghost/server), many binaries under cmd/, all sharing
# the packages at the module root (auth, profile, pair, models, hw, server, ...). Binaries land in
# bin/ with the names systemd expects.
#
# The front door is ghost.secd (cmd/secd). The other daemons (cmd/voiced, cmd/framed, ...) are added
# as cmd/ entries and picked up automatically by `make daemons`.

GO      ?= go
BINDIR  := bin
TAGS    ?=

# The front-door daemon and the operator tools. cmd/<x> -> bin/ghost.<x> for daemons, plain names
# for the tools, matching the systemd unit ExecStart (execDir/ghost.secd) and the GhostDaemons list.
.PHONY: all
all: secd setup ctl

# secd is built with the binary name the service unit references.
.PHONY: secd
secd:
	$(GO) build -tags '$(TAGS)' -o $(BINDIR)/ghost.secd ./cmd/secd

.PHONY: setup
setup:
	$(GO) build -o $(BINDIR)/ghost-setup ./cmd/setup

.PHONY: ctl
ctl:
	$(GO) build -o $(BINDIR)/ghost-ctl ./cmd/ctl

# daemons builds every backing daemon that exists under cmd/ (voiced, framed, ...) into
# bin/ghost.<name>, skipping secd/setup/ctl which have explicit targets above. As you add cmd/voiced
# etc. this picks them up with no Makefile change.
.PHONY: daemons
daemons:
	@for d in cmd/*/; do \
		name=$$(basename $$d); \
		case $$name in secd|setup|ctl) continue;; esac; \
		echo "building ghost.$$name"; \
		$(GO) build -tags '$(TAGS)' -o $(BINDIR)/ghost.$$name ./cmd/$$name; \
	done

# The real box build links the TPM/dm-crypt/datastore backend. Needs go-tpm in the module.
.PHONY: box
box: TAGS=tpm
box: secd daemons
	$(GO) build -o $(BINDIR)/ghost-setup ./cmd/setup
	$(GO) build -o $(BINDIR)/ghost-ctl ./cmd/ctl

.PHONY: test
test:
	$(GO) test ./...

.PHONY: clean
clean:
	rm -rf $(BINDIR)
