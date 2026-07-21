package framed

// BACKUPS , the second copy of the irreplaceable bytes. Design rules, in order:
//
//  1. ASYMMETRIC OR NOTHING. The nightly job holds only a RECIPIENT PUBLIC KEY
//     (/var/lib/ghost/backup.pub, 64 hex chars, placed by the operator). It can write backups it
//     can never read; the private key lives offline with the human. A stolen box plus a stolen
//     backup disk yields ciphertext twice.
//  2. OPT-IN BY KEY. No backup.pub, no backups , one log line at startup, then silence. The
//     operator who has not made a key has not decided where their second copy lives, and a backup
//     system that guesses is a backup system that leaks.
//  3. The app cannot reach the backup directory , it is outside every served mount path, and the
//     appears-down surface has no route near it.
//  4. WEEKLY FULL, DAILY INCREMENTAL. Sunday (or first ever run) tars the whole framed tree;
//     other nights tar only files modified since the last backup watermark. Four fulls are kept;
//     incrementals older than the oldest kept full go with it.
//
// Seal format ("GHSTBK1"): magic || ephemeral X25519 pub (32) || chunks. Per file: fresh ephemeral
// key, shared = ECDH(ephemeral, recipient), key = HKDF-SHA256(shared, salt=ephPub||recipPub,
// info="ghost-backup-v1"). Chunks: uint32 BE ciphertext length || ChaCha20-Poly1305 ciphertext of
// up to 1 MiB plaintext. Nonce: bytes 0..7 = BE64 chunk index, byte 11 = 0x01 on the final chunk
// (a truncated file fails authentication instead of decrypting to a shorter archive). This is the
// same hand-rolled STREAM shape used elsewhere in the repo , stdlib + x/crypto only, no new deps.

import (
	"archive/tar"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const backupMagic = "GHSTBK1"
const backupChunk = 1 << 20 // 1 MiB plaintext per sealed chunk

// sealedWriter encrypts a stream to a recipient public key in authenticated chunks.
type sealedWriter struct {
	w     io.Writer
	aead  interface {
		Seal(dst, nonce, plaintext, additionalData []byte) []byte
		NonceSize() int
	}
	buf   []byte
	n     int
	chunk uint64
}

func newSealedWriter(w io.Writer, recipientPub []byte) (*sealedWriter, error) {
	if len(recipientPub) != 32 {
		return nil, fmt.Errorf("recipient public key must be 32 bytes, got %d", len(recipientPub))
	}
	curve := ecdh.X25519()
	eph, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("ephemeral key: %w", err)
	}
	rp, err := curve.NewPublicKey(recipientPub)
	if err != nil {
		return nil, fmt.Errorf("recipient key: %w", err)
	}
	shared, err := eph.ECDH(rp)
	if err != nil {
		return nil, fmt.Errorf("ecdh: %w", err)
	}
	ephPub := eph.PublicKey().Bytes()
	salt := append(append([]byte{}, ephPub...), recipientPub...)
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, shared, salt, []byte("ghost-backup-v1")), key); err != nil {
		return nil, fmt.Errorf("hkdf: %w", err)
	}
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write([]byte(backupMagic)); err != nil {
		return nil, err
	}
	if _, err := w.Write(ephPub); err != nil {
		return nil, err
	}
	return &sealedWriter{w: w, aead: aead, buf: make([]byte, backupChunk)}, nil
}

func (s *sealedWriter) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		c := copy(s.buf[s.n:], p)
		s.n += c
		p = p[c:]
		total += c
		if s.n == len(s.buf) {
			if err := s.flush(false); err != nil {
				return total, err
			}
		}
	}
	return total, nil
}

func (s *sealedWriter) flush(final bool) error {
	nonce := make([]byte, s.aead.NonceSize())
	binary.BigEndian.PutUint64(nonce[:8], s.chunk)
	if final {
		nonce[11] = 1
	}
	ct := s.aead.Seal(nil, nonce, s.buf[:s.n], nil)
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(ct)))
	if _, err := s.w.Write(lenBuf[:]); err != nil {
		return err
	}
	if _, err := s.w.Write(ct); err != nil {
		return err
	}
	s.chunk++
	s.n = 0
	return nil
}

// Close seals the final (possibly empty) chunk , the 0x01 nonce byte is the end-of-stream mark.
func (s *sealedWriter) Close() error { return s.flush(true) }

// BackupConfig , where the second copy lives and who can read it.
type BackupConfig struct {
	Dir     string // e.g. /var/lib/ghost/backup (operator mounts the HDD here)
	PubFile string // e.g. /var/lib/ghost/backup.pub (64 hex chars, X25519 public key)
}

// RunBackup writes one sealed tar of the framed tree under root (the mount's framed dir). Full
// backups take everything; incrementals take files with mtime after `since`. Returns the sealed
// file path, file count, and byte count of plaintext archived.
func RunBackup(cfg BackupConfig, root string, full bool, since time.Time, lg *slog.Logger) (string, int, int64, error) {
	pubHex, err := os.ReadFile(cfg.PubFile)
	if err != nil {
		return "", 0, 0, fmt.Errorf("no recipient key (%s): %w", cfg.PubFile, err)
	}
	pub, err := hex.DecodeString(strings.TrimSpace(string(pubHex)))
	if err != nil || len(pub) != 32 {
		return "", 0, 0, fmt.Errorf("backup.pub must be 64 hex chars of X25519 public key")
	}
	if err := os.MkdirAll(cfg.Dir, 0o700); err != nil {
		return "", 0, 0, err
	}
	kind := "incr"
	if full {
		kind = "full"
	}
	name := fmt.Sprintf("%s-%s.tar.seal", kind, time.Now().Format("2006-01-02-1504"))
	tmp := filepath.Join(cfg.Dir, "."+name+".tmp")
	out := filepath.Join(cfg.Dir, name)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", 0, 0, err
	}
	defer f.Close()
	sw, err := newSealedWriter(f, pub)
	if err != nil {
		return "", 0, 0, err
	}
	tw := tar.NewWriter(sw)
	files, bytes := 0, int64(0)
	walkErr := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if !full && !info.ModTime().After(since) {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		hdr, herr := tar.FileInfoHeader(info, "")
		if herr != nil {
			return herr
		}
		hdr.Name = rel
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		src, oerr := os.Open(p)
		if oerr != nil {
			// A file that vanished mid-walk (rare, live system) is logged and skipped , the
			// backup should carry everything it can, not die on the first race.
			lg.Warn("backup skip", "fn", "RunBackup", "path", p, "err", oerr)
			return nil
		}
		n, cerr := io.Copy(tw, src)
		src.Close()
		if cerr != nil {
			return cerr
		}
		files++
		bytes += n
		return nil
	})
	if walkErr != nil {
		return "", files, bytes, walkErr
	}
	if err := tw.Close(); err != nil {
		return "", files, bytes, err
	}
	if err := sw.Close(); err != nil {
		return "", files, bytes, err
	}
	if err := f.Sync(); err != nil {
		return "", files, bytes, err
	}
	f.Close()
	if err := os.Rename(tmp, out); err != nil {
		return "", files, bytes, err
	}
	if full {
		pruneBackups(cfg.Dir, lg)
	}
	return out, files, bytes, nil
}

// pruneBackups keeps the newest 4 fulls; incrementals older than the oldest kept full go with it.
func pruneBackups(dir string, lg *slog.Logger) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var fulls []string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "full-") && strings.HasSuffix(e.Name(), ".tar.seal") {
			fulls = append(fulls, e.Name())
		}
	}
	sort.Strings(fulls) // names embed the date, so lexical order is chronological
	if len(fulls) <= 4 {
		return
	}
	cutoff := fulls[len(fulls)-4]
	for _, e := range ents {
		n := e.Name()
		if !strings.HasSuffix(n, ".tar.seal") {
			continue
		}
		// Anything (full or incr) lexically older than the oldest kept full is out of its
		// restore chain and can go.
		if n < cutoff {
			if err := os.Remove(filepath.Join(dir, n)); err != nil {
				lg.Warn("prune failed", "fn", "pruneBackups", "file", n, "err", err)
			} else {
				lg.Info("pruned old backup", "fn", "pruneBackups", "file", n)
			}
		}
	}
}
