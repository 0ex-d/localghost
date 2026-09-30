// Package harden holds the few process-level settings every LocalGhost daemon makes first.
package harden

import "syscall"

// NoDump marks the process not dumpable (prctl PR_SET_DUMPABLE 0). Two doors close with it:
//   - another process of the SAME user can no longer open /proc/<pid>/root, /proc/<pid>/mem or
//     /proc/<pid>/environ, or ptrace it. secd's private mount namespace hides the decrypted volume
//     from the host, but not from a process running as the daemons' own user, which reached it
//     through /proc/<daemon pid>/root (proved 30 Sep 2026 with a tmpfs in a private namespace:
//     the host saw 0 files, a same-uid shell read the file);
//   - no core file of a process holding decrypted data.
//
// It does not cover the programs the daemons start (Postgres, Redis, llama-server, whisper-cli,
// ffmpeg): exec resets the flag. Those need the cohort to run as a user nothing else runs as.
func NoDump() {
	_, _, _ = syscall.RawSyscall(syscall.SYS_PRCTL, syscall.PR_SET_DUMPABLE, 0, 0)
}
