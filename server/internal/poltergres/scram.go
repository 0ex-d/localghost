package poltergres

// SCRAM-SHA-256 client (RFC 5802 mechanics, RFC 7677 hash suite), written as a pure state machine so
// the test can drive it against the RFC's published vector without a server. Postgres notes: the
// username in the SCRAM messages is empty (the server takes it from the startup packet), and channel
// binding is "n" (not attempted , no TLS on a volume-local socket, so SCRAM-SHA-256-PLUS is moot).
//
// Our passwords are randHex from provisioning, so SASLprep normalisation is a no-op for every
// password this box will ever generate; stated rather than silently assumed.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

type scram struct {
	pass        string
	user        string // empty for Postgres; settable for the RFC test vector
	nonce       string
	firstBare   string
	authMessage string
	serverKey   []byte
}

func newScram(pass string) (*scram, error) {
	var nb [18]byte
	if _, err := rand.Read(nb[:]); err != nil {
		return nil, err
	}
	s := &scram{pass: pass, nonce: base64.StdEncoding.EncodeToString(nb[:])}
	return s, nil
}

// clientFirst returns the client-first-message (gs2 header + bare part).
func (s *scram) clientFirst() string {
	s.firstBare = "n=" + s.user + ",r=" + s.nonce
	return "n,," + s.firstBare
}

// clientFinal consumes the server-first-message and returns the client-final-message with proof.
func (s *scram) clientFinal(serverFirst string) (string, error) {
	attrs := parseScramAttrs(serverFirst)
	serverNonce, okR := attrs["r"]
	saltB64, okS := attrs["s"]
	iterStr, okI := attrs["i"]
	if !okR || !okS || !okI {
		return "", errors.New("scram: malformed server-first")
	}
	if !strings.HasPrefix(serverNonce, s.nonce) {
		return "", errors.New("scram: server nonce does not extend client nonce")
	}
	salt, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		return "", fmt.Errorf("scram salt: %w", err)
	}
	iters, err := strconv.Atoi(iterStr)
	if err != nil || iters < 1 {
		return "", errors.New("scram: bad iteration count")
	}

	salted := pbkdf2.Key([]byte(s.pass), salt, iters, sha256.Size, sha256.New)
	clientKey := hmacSHA256(salted, "Client Key")
	storedKey := sha256.Sum256(clientKey)
	s.serverKey = hmacSHA256(salted, "Server Key")

	finalNoProof := "c=biws,r=" + serverNonce // biws = base64("n,,")
	s.authMessage = s.firstBare + "," + serverFirst + "," + finalNoProof

	sig := hmacSHA256(storedKey[:], s.authMessage)
	proof := make([]byte, len(clientKey))
	for i := range proof {
		proof[i] = clientKey[i] ^ sig[i]
	}
	return finalNoProof + ",p=" + base64.StdEncoding.EncodeToString(proof), nil
}

// verifyServer checks the server-final v= signature , mutual auth, the server proves it holds the
// verifier rather than just shrugging our proof through.
func (s *scram) verifyServer(serverFinal string) error {
	attrs := parseScramAttrs(serverFinal)
	v, ok := attrs["v"]
	if !ok {
		if e, bad := attrs["e"]; bad {
			return errors.New("scram server error: " + e)
		}
		return errors.New("scram: no server signature")
	}
	want := base64.StdEncoding.EncodeToString(hmacSHA256(s.serverKey, s.authMessage))
	if !hmac.Equal([]byte(v), []byte(want)) {
		return errors.New("scram: server signature mismatch")
	}
	return nil
}

func hmacSHA256(key []byte, msg string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(msg))
	return h.Sum(nil)
}

// parseScramAttrs splits "k=v,k=v" , values may themselves contain '=' (base64), so split on the
// FIRST '=' only.
func parseScramAttrs(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		if i := strings.IndexByte(part, '='); i > 0 {
			out[part[:i]] = part[i+1:]
		}
	}
	return out
}
