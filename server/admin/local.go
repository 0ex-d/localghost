package admin

import (
	"errors"
	"net"
	"strings"
)

// Rotation (resetup) is a destructive root operation. It is reachable ONLY from the local network,
// never remotely, so a coercer with the phone cannot reach it and a remote attacker cannot either.
// This is enforced at the point of the dangerous action (not only in sshd config) as defence in
// depth: even if sshd drifts to listening publicly, the command refuses a non-local session.
//
// FAIL CLOSED: if the session's origin cannot be determined or is not provably local, refuse. We do
// not trust SSH_CLIENT blindly, do not use reverse DNS, and do not treat "a private IP exists on the
// box" as proof. We test the actual peer address of THIS session against loopback and the private
// ranges, and reject everything else, including anything ambiguous.
//
// Honest limit: a private source address means "on the local network", not "physically on my LAN".
// A VPN or port-forward that lands a remote peer in a private range would pass. For this threat
// model that is acceptable (anyone already inside the LAN/VPN has broad access); the docs say
// "requires a local-network connection", not "impossible remotely". Tighten to LoopbackOnly if you
// want SSH-to-localhost / console only.

var ErrNotLocal = errors.New("rotation refused: this command must be run from the local network")

// privateBlocks are loopback + RFC1918 + link-local + unique-local IPv6.
var privateBlocks = func() []*net.IPNet {
	cidrs := []string{
		"127.0.0.0/8",    // IPv4 loopback
		"10.0.0.0/8",     // RFC1918
		"172.16.0.0/12",  // RFC1918
		"192.168.0.0/16", // RFC1918
		"169.254.0.0/16", // link-local
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 unique local
		"fe80::/10",      // IPv6 link-local
	}
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}()

// IsLocalAddr reports whether a peer address (host or host:port) is a local-network address. Returns
// false for anything it cannot parse or that is not in a private/loopback range , fail closed.
func IsLocalAddr(peer string) bool {
	host := peer
	if h, _, err := net.SplitHostPort(peer); err == nil {
		host = h
	}
	host = strings.TrimSpace(host)
	ip := net.ParseIP(host)
	if ip == nil {
		return false // unparseable => not provably local => refuse
	}
	for _, b := range privateBlocks {
		if b.Contains(ip) {
			return true
		}
	}
	return false
}

// SessionPeer extracts the SSH session's peer address. It prefers an explicitly passed connection
// peer (the real RemoteAddr, the trustworthy source); sshClientEnv is the SSH_CLIENT value as a
// fallback only. Either way the address is then run through IsLocalAddr. Returns the host and whether
// it could be determined at all.
func SessionPeer(connPeer string, sshClientEnv string) (string, bool) {
	if connPeer != "" {
		return connPeer, true
	}
	// SSH_CLIENT is "<client-ip> <client-port> <server-port>"; take the first field.
	if f := strings.Fields(sshClientEnv); len(f) > 0 {
		return f[0], true
	}
	return "", false
}

// RequireLocal is the gate the resetup commands call first. It returns ErrNotLocal unless the
// session origin is determinable AND local. Fail closed on every uncertainty.
func RequireLocal(connPeer, sshClientEnv string) error {
	host, ok := SessionPeer(connPeer, sshClientEnv)
	if !ok {
		return ErrNotLocal // cannot determine origin => refuse
	}
	if !IsLocalAddr(host) {
		return ErrNotLocal
	}
	return nil
}
