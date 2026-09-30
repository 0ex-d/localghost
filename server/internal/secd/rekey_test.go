package secd

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// a box CA on disk, the way setup writes it, and a device certificate "from the QR"
func testCA(t *testing.T, dir string) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "box CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kder, _ := x509.MarshalECPrivateKey(key)
	os.WriteFile(filepath.Join(dir, "box-ca.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
	os.WriteFile(filepath.Join(dir, "box-ca-key.pem"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder}), 0o600)
	ca, _ := x509.ParseCertificate(der)
	return ca, key
}

// the header nginx sets for a verified client certificate
func escapedPEM(der []byte) string {
	return url.QueryEscape(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})))
}

func TestDeviceKeyRotation(t *testing.T) {
	caDir := t.TempDir()
	ca, caKey := testCA(t, caDir)
	qrKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	qrDER, err := issueDeviceCert(ca, caKey, "vlad-phone", &qrKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(Config{StateDir: t.TempDir(), CaDir: caDir})
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.mounted = 0
	s.mu.Unlock()
	mount := filepath.Join(s.cfg.StateDir, "mnt", "slot0")
	os.MkdirAll(mount, 0o755)
	tok, _ := s.session.Issue()
	call := func(path, cert string, body []byte) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("POST", path, bytes.NewReader(body))
		if path == "/v1/health" {
			req = httptest.NewRequest("GET", path, nil)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		if cert != "" {
			req.Header.Set("X-Client-Cert", cert)
		}
		s.Handler().ServeHTTP(rr, req)
		return rr
	}
	oldHdr := escapedPEM(qrDER)

	// the phone's trail key is filed under its current certificate
	tk, _ := ecdh.X25519().GenerateKey(rand.Reader)
	oldReq := httptest.NewRequest("GET", "/", nil)
	oldReq.Header.Set("X-Client-Cert", oldHdr)
	if err := saveTrailKey(mount, deviceKeyFromRequest(oldReq), tk); err != nil {
		t.Fatal(err)
	}

	// the phone's own key, and the proof it holds it
	phoneKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	spki, _ := x509.MarshalPKIXPublicKey(&phoneKey.PublicKey)
	h := sha256.Sum256(append([]byte(rekeyMessage), spki...))
	sig, _ := ecdsa.SignASN1(rand.Reader, phoneKey, h[:])
	enc := base64.StdEncoding.EncodeToString

	// a proof made with another key is refused
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	badSig, _ := ecdsa.SignASN1(rand.Reader, other, h[:])
	bad, _ := json.Marshal(map[string]string{"spki": enc(spki), "sig": enc(badSig)})
	if rr := call("/v1/device/rekey", oldHdr, bad); rr.Code != 400 {
		t.Fatalf("a bad proof: %d", rr.Code)
	}
	// no client certificate, no rotation
	good, _ := json.Marshal(map[string]string{"spki": enc(spki), "sig": enc(sig)})
	if rr := call("/v1/device/rekey", "", good); rr.Code == 200 {
		t.Fatal("rotated without a client certificate")
	}

	rr := call("/v1/device/rekey", oldHdr, good)
	if rr.Code != 200 {
		t.Fatalf("rekey: %d %s", rr.Code, rr.Body)
	}
	var got struct {
		Cert string `json:"cert"`
	}
	json.Unmarshal(rr.Body.Bytes(), &got)
	blk, _ := pem.Decode([]byte(got.Cert))
	if blk == nil {
		t.Fatalf("no certificate: %s", rr.Body)
	}
	nc, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := nc.CheckSignatureFrom(ca); err != nil {
		t.Fatalf("not signed by the box CA: %v", err)
	}
	if !nc.PublicKey.(*ecdsa.PublicKey).Equal(&phoneKey.PublicKey) || nc.Subject.CommonName != "vlad-phone" {
		t.Fatal("the certificate is not for the phone's key, or lost its name")
	}
	// until the phone confirms, the QR's certificate still works
	if rr := call("/v1/health", oldHdr, nil); rr.Code != 200 {
		t.Fatalf("old certificate before confirm: %d", rr.Code)
	}
	newHdr := escapedPEM(blk.Bytes)
	if rr := call("/v1/device/rekey/confirm", newHdr, nil); rr.Code != 200 || strings.Contains(rr.Body.String(), "already") {
		t.Fatalf("confirm: %d %s", rr.Code, rr.Body)
	}
	if rr := call("/v1/health", oldHdr, nil); rr.Code == 200 {
		t.Fatal("the retired certificate still reaches the box")
	}
	if rr := call("/v1/unlock", oldHdr, []byte(`{"pin":"123456"}`)); rr.Code == 200 || rr.Code == 202 {
		t.Fatal("the retired certificate can still try PINs")
	}
	if rr := call("/v1/health", newHdr, nil); rr.Code != 200 {
		t.Fatalf("new certificate: %d", rr.Code)
	}
	// the trail key followed the phone
	newReq := httptest.NewRequest("GET", "/", nil)
	newReq.Header.Set("X-Client-Cert", newHdr)
	if _, err := loadTrailKey(mount, deviceKeyFromRequest(newReq)); err != nil {
		t.Fatalf("trail key did not follow: %v", err)
	}
	// a second confirm (a lost answer) is fine
	if rr := call("/v1/device/rekey/confirm", newHdr, nil); rr.Code != 200 {
		t.Fatalf("second confirm: %d", rr.Code)
	}
	// retired survives a restart of secd, and the file holds fingerprints only
	s2, _ := New(Config{StateDir: s.cfg.StateDir, CaDir: caDir})
	rr2 := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/health", nil)
	req.Header.Set("X-Client-Cert", oldHdr)
	s2.Handler().ServeHTTP(rr2, req)
	if rr2.Code == 200 {
		t.Fatal("retired forgotten across a restart")
	}
	b, _ := os.ReadFile(filepath.Join(s.cfg.StateDir, "devices", "retired"))
	if fi, _ := os.Stat(filepath.Join(s.cfg.StateDir, "devices", "retired")); fi.Mode().Perm() != 0o600 || strings.Contains(string(b), "BEGIN") {
		t.Fatalf("retired file: %v %q", fi.Mode(), b)
	}
}
