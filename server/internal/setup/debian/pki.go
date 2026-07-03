package debian

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// The box is its own CA. This file issues, in pure Go (crypto/x509, no openssl shell-out):
//   - the box CA
//   - the box's HTTPS server cert (signed by the CA), which the phone pins by fingerprint
//   - a device client cert (signed by the CA), delivered to the phone via the QR
//
// Files land in <caDir> (e.g. /etc/ghost/ca), the paths the nginx config references:
//   box-ca.pem / box-ca-key.pem        the CA
//   box-server.pem / box-server-key.pem the server cert
//   devices-ca.pem                      the CA bundle nginx verifies clients against (== box CA)
//   device-<name>.pem / device-<name>-key.pem  an issued device identity

type PKI struct {
	caDir string
	host  string // the box hostname/IP the server cert is valid for
}

func NewPKI(caDir, host string) *PKI { return &PKI{caDir: caDir, host: host} }

func (p PKI) caCertPath() string    { return filepath.Join(p.caDir, "box-ca.pem") }
func (p PKI) caKeyPath() string     { return filepath.Join(p.caDir, "box-ca-key.pem") }
func (p PKI) serverCertPath() string { return filepath.Join(p.caDir, "box-server.pem") }
func (p PKI) serverKeyPath() string  { return filepath.Join(p.caDir, "box-server-key.pem") }
func (p PKI) devicesCAPath() string  { return filepath.Join(p.caDir, "devices-ca.pem") }

// Exists reports whether the CA is already present (idempotent setup).
func (p PKI) Exists() bool {
	_, err := os.Stat(p.caCertPath())
	return err == nil
}

// CreateCA generates the box CA and writes it, plus the devices-ca bundle nginx verifies against
// (which is the same CA, since the box signs its own device certs).
func (p PKI) CreateCA() error {
	if err := os.MkdirAll(p.caDir, 0o700); err != nil {
		return err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	// The CA's subject becomes the ISSUER field of the server cert presented to every TLS client ,
	// including every internet scanner. Appears-down at the HTTP layer is worthless if the
	// certificate announces the product in the handshake, so the name is deliberately generic:
	// "ca" looks like any of a million homelab and appliance CAs and identifies nothing.
	tmpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: "ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(20, 0, 0), // long-lived; the box owns it
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	if err := writeCertPEM(p.caCertPath(), der); err != nil {
		return err
	}
	if err := writeKeyPEM(p.caKeyPath(), key); err != nil {
		return err
	}
	// nginx verifies client certs against the box CA.
	return writeCertPEM(p.devicesCAPath(), der)
}

// IssueServerCert signs the box's HTTPS server cert for its host (IP or name).
func (p PKI) IssueServerCert() error {
	ca, caKey, err := p.loadCA()
	if err != nil {
		return err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: p.host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(p.host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{p.host}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	if err := writeCertPEM(p.serverCertPath(), der); err != nil {
		return err
	}
	return writeKeyPEM(p.serverKeyPath(), key)
}

// IssueDeviceCertDER signs a client cert and returns the cert + PKCS8 key as raw DER, for delivery
// inside the enrolment QR. The private key is NEVER written to disk: in the QR-delivery model the
// phone holds the only durable copy, and "the box wipes its copy" has to be true on the filesystem,
// not just in prose. The CERT is recorded (public material) at device-issued.pem, overwritten on
// each issuance , the record shows the LATEST identity, which is the rotation model: render a new
// QR, get a fresh identity, the phone overwrites its stored one on enrol. Stated plainly: there is
// no CRL, so an OLD issued cert stays TLS-valid until the CA itself is rotated; the second gate
// (PIN, rate-limited) is what an old credential still runs into. No name parameter: the server does
// not care which phone this is , naming is app-side metadata , and a fixed tiny CN keeps DER bytes
// out of the QR budget.
func (p PKI) IssueDeviceCertDER() (certDER, keyDER []byte, err error) {
	ca, caKey, err := p.loadCA()
	if err != nil {
		return nil, nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: "device"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	certDER, err = x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err = x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	// Record the CERT only. No key file: grep this directory after setup and you will find no
	// device key for QR-issued identities , that absence is the design, not an omission.
	_ = writeCertPEM(filepath.Join(p.caDir, "device-issued.pem"), certDER)
	return certDER, keyDER, nil
}

// ServerFingerprint returns the SHA-256 of the server cert DER as uppercase colon-hex, the value the
// phone pins (and that travels in the QR).
func (p PKI) ServerFingerprint() (string, error) {
	b, err := os.ReadFile(p.serverCertPath())
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return "", fmt.Errorf("server cert not PEM")
	}
	sum := sha256.Sum256(block.Bytes)
	out := make([]byte, 0, 95)
	const hex = "0123456789ABCDEF"
	for i, x := range sum {
		if i > 0 {
			out = append(out, ':')
		}
		out = append(out, hex[x>>4], hex[x&0x0f])
	}
	return string(out), nil
}

func (p PKI) loadCA() (*x509.Certificate, *ecdsa.PrivateKey, error) {
	cb, err := os.ReadFile(p.caCertPath())
	if err != nil {
		return nil, nil, err
	}
	kb, err := os.ReadFile(p.caKeyPath())
	if err != nil {
		return nil, nil, err
	}
	cblock, _ := pem.Decode(cb)
	kblock, _ := pem.Decode(kb)
	if cblock == nil || kblock == nil {
		return nil, nil, fmt.Errorf("CA files not PEM")
	}
	cert, err := x509.ParseCertificate(cblock.Bytes)
	if err != nil {
		return nil, nil, err
	}
	key, err := x509.ParseECPrivateKey(kblock.Bytes)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func serial() *big.Int {
	n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	return n
}

func writeCertPEM(path string, der []byte) error {
	return os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}

func writeKeyPEM(path string, key *ecdsa.PrivateKey) error {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	return os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600)
}
