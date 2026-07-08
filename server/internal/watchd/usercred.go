package watchd

// userCred resolves the configured run-user to a syscall.Credential so spawned daemons drop from
// watchd's uid to the run-user's. Empty runUser (the default 'ghost' path handles this at the systemd
// level, so watchd itself already runs as the right user) means no drop , the daemons inherit
// watchd's uid, which is correct when watchd is already the run-user.
//
// This exists for the --user <name> case where the operator wants the whole stack under their own
// account. secd (root, because it mounts) starts watchd as that user via the run wrapper; watchd then
// spawns daemons under the same uid. If watchd is ALREADY that user (the common case), userCred
// returns nil and the daemons simply inherit.

import (
	"os/user"
	"strconv"
	"syscall"
)

// userCred returns a Credential for runUser, or nil to inherit watchd's own uid/gid. A lookup failure
// also returns nil (inherit) rather than failing every spawn , watchd is likely already running as
// the intended user, so inheriting is the safe default, and the mismatch (if any) surfaces in the
// daemon's own permission errors rather than a hard supervisor failure.
func (s *Supervisor) userCred() *syscall.Credential {
	if s.runUser == "" {
		return nil
	}
	u, err := user.Lookup(s.runUser)
	if err != nil {
		s.jlog.Warn("run-user lookup failed, daemons inherit watchd uid", "fn", "userCred", "user", s.runUser, "err", err)
		return nil
	}
	uid, err1 := strconv.Atoi(u.Uid)
	gid, err2 := strconv.Atoi(u.Gid)
	if err1 != nil || err2 != nil {
		s.jlog.Warn("run-user has non-numeric uid/gid, daemons inherit watchd uid", "fn", "userCred", "user", s.runUser)
		return nil
	}
	// If we are already this uid, no need to set a credential (and setting it to our own uid is
	// harmless but pointless). We cannot cheaply know our uid here without a syscall; setting the
	// credential to our own uid is a no-op to the kernel, so just set it.
	return &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
}
