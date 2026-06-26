package admin

import (
	"errors"
	"strings"
)

// Rotation is gated on a local session, but defence in depth says: also make sure sshd is not
// listening on a public interface in the first place. Setup verifies this and warns loudly if it is,
// because an SSH daemon reachable from the internet is a far bigger door than rotation alone.
//
// This inspects the effective ListenAddress lines. The real implementation reads `sshd -T` output
// (the resolved config); ParseListenAddresses is the pure check over those lines so it is testable.

var ErrSSHPublic = errors.New("sshd is listening on a public address; bind it to the local network")

// ParseListenAddresses takes the listenaddress lines from `sshd -T` (lowercased keyword + value) and
// returns the addresses. sshd -T prints one "listenaddress <host>:<port>" per binding.
func ParseListenAddresses(sshdTOutput string) []string {
	var addrs []string
	for _, line := range strings.Split(sshdTOutput, "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) >= 2 && strings.EqualFold(f[0], "listenaddress") {
			addrs = append(addrs, f[1])
		}
	}
	return addrs
}

// SSHDIsLocalOnly reports whether every sshd listen address is local (loopback/RFC1918/link-local).
// A wildcard bind (0.0.0.0 / ::) counts as public. Empty input is treated as public/unknown , fail
// closed , because "no explicit bind" usually means "listen on everything".
func SSHDIsLocalOnly(sshdTOutput string) (bool, []string) {
	addrs := ParseListenAddresses(sshdTOutput)
	if len(addrs) == 0 {
		return false, nil // unknown => treat as not-local-only
	}
	var public []string
	for _, a := range addrs {
		host := a
		if i := strings.LastIndex(a, ":"); i >= 0 && strings.Count(a, ":") == 1 {
			host = a[:i] // ipv4:port
		}
		host = strings.Trim(host, "[]")
		// Wildcards are public.
		if host == "0.0.0.0" || host == "::" || host == "*" || host == "" {
			public = append(public, a)
			continue
		}
		if !IsLocalAddr(host) {
			public = append(public, a)
		}
	}
	return len(public) == 0, public
}
