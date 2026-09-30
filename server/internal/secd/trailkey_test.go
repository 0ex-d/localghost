package secd

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrailSealRoundTrip(t *testing.T) {
	k, _ := ecdh.X25519().GenerateKey(rand.Reader)
	line, err := sealTrail(k.PublicKey(), "1790000000 38.2 20.6 12")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "s1:") || strings.Contains(line, "38.2") {
		t.Fatalf("not sealed: %s", line)
	}
	got, err := openTrail(k, line)
	if err != nil || got != "1790000000 38.2 20.6 12" {
		t.Fatalf("open: %q %v", got, err)
	}
	other, _ := ecdh.X25519().GenerateKey(rand.Reader)
	if _, err := openTrail(other, line); err == nil {
		t.Fatal("opened with another key")
	}
	// one flipped byte anywhere and it does not open
	raw, _ := base64.StdEncoding.DecodeString(line[3:])
	raw[len(raw)-1] ^= 1
	if _, err := openTrail(k, "s1:"+base64.StdEncoding.EncodeToString(raw)); err == nil {
		t.Fatal("a tampered line opened")
	}
}

// The phone's own sealing (sync/TrailSeal.kt, run on a JVM) opens here: the two sides agree.
func TestTrailOpensWhatThePhoneSealed(t *testing.T) {
	k, err := trailKeyFrom(phoneVectorPub, phoneVectorPriv)
	if err != nil {
		t.Fatal(err)
	}
	got, err := openTrail(k, phoneVectorLine)
	if err != nil || got != phoneVectorPlain {
		t.Fatalf("the phone's line: %q %v", got, err)
	}
}

func TestTrailKeyMustMatch(t *testing.T) {
	a, _ := ecdh.X25519().GenerateKey(rand.Reader)
	b, _ := ecdh.X25519().GenerateKey(rand.Reader)
	enc := base64.StdEncoding.EncodeToString
	if _, err := trailKeyFrom(enc(a.PublicKey().Bytes()), enc(b.Bytes())); err == nil {
		t.Fatal("a mismatched pair was taken")
	}
	if _, err := trailKeyFrom(enc(a.PublicKey().Bytes()), enc(a.Bytes())); err != nil {
		t.Fatal(err)
	}
}

func TestTrailKeyStaysSecds(t *testing.T) {
	mount := t.TempDir()
	k, _ := ecdh.X25519().GenerateKey(rand.Reader)
	if err := saveTrailKey(mount, "abcd1234abcd1234", k); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(trailKeyPath(mount, "abcd1234abcd1234"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode %v %v", fi, err)
	}
	if di, _ := os.Stat(filepath.Join(mount, "secd")); di.Mode().Perm() != 0o700 {
		t.Fatalf("secd dir mode %v", di.Mode())
	}
	got, err := loadTrailKey(mount, "abcd1234abcd1234")
	if err != nil || string(got.Bytes()) != string(k.Bytes()) {
		t.Fatalf("load: %v", err)
	}
	// a rekeyed device keeps its key under its new name
	if err := moveTrailKey(mount, "abcd1234abcd1234", "ffff0000ffff0000"); err != nil {
		t.Fatal(err)
	}
	if _, err := loadTrailKey(mount, "ffff0000ffff0000"); err != nil {
		t.Fatal("key did not follow the device")
	}
}

func TestLocationBatchOpened(t *testing.T) {
	k, _ := ecdh.X25519().GenerateKey(rand.Reader)
	s1, _ := sealTrail(k.PublicKey(), "1790000000 38.25 20.625 9")
	s2, _ := sealTrail(k.PublicKey(), "1790000900 38.26 20.63")
	other, _ := ecdh.X25519().GenerateKey(rand.Reader)
	s3, _ := sealTrail(other.PublicKey(), "1790001800 1 2")
	body, _ := json.Marshal(map[string]any{"source": "phone-ab", "points": []any{map[string]any{"ts": 1789999000, "lat": 1.0, "lon": 2.0}},
		"sealed": []string{s1, s2, s3}})
	out, bad, err := openLocationBatch(body, k)
	if err != nil || bad != 1 {
		t.Fatalf("bad %d err %v", bad, err)
	}
	var got struct {
		Source string `json:"source"`
		Points []struct {
			TS  int64   `json:"ts"`
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
		} `json:"points"`
		Sealed []string `json:"sealed"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Source != "phone-ab" || len(got.Points) != 3 || got.Sealed != nil || got.Points[1].TS != 1790000000 || got.Points[1].Lat != 38.25 {
		t.Fatalf("batch %s", out)
	}
	if strings.Contains(string(out), "s1:") {
		t.Fatal("a sealed line reached the spool")
	}
	if _, _, err := openLocationBatch(body, nil); err != errNoTrailKey {
		t.Fatalf("no key: %v", err)
	}
	plain, _ := json.Marshal(map[string]any{"source": "phone-ab", "points": []any{}})
	if out, _, err := openLocationBatch(plain, nil); err != nil || string(out) != string(plain) {
		t.Fatal("a plain batch was changed")
	}
}

// sealed by sync/TrailSeal.kt on a JVM (the phone's code); TrailSealTest.opensWhatTheBoxSealed is
// the same check the other way
const (
	phoneVectorPub   = "utXLuNxZp28PxC/AIjmhDu03cxihveNblWY431hrlkY="
	phoneVectorPriv  = "zL/+L4/QLYCH1hyfZuo+ixl0BA105RPhcPDJL7URdm8="
	phoneVectorLine  = "s1:miuwAb9xVqCd9NiXjJjchxJkin+TFf6OINyjAujeH1gvhcZ8wxT3vXmCh+S6qc+C8JJs1s4i056rs01JMhqUV1zPWCU17yXO5BNhEGbRCBQiQ6lhAvA="
	phoneVectorPlain = "1790007200 45.4642 9.19 15"
)
