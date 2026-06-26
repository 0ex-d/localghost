package profile

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
)

// Registry persistence. The on-disk form contains ALL RegistrySize entries , real ones and random
// filler , in a fixed layout, so the file is always the same size and an attacker who reads it
// cannot tell how many real PINs exist (the count-hiding property that gives structural
// deniability). Each entry is: hashLen bytes of hash, then two int32 (open, wipe). The salt is
// stored first so the same box salt is used across restarts.
//
// The file lives in the UNENCRYPTED state area (it must be readable before any account is unlocked,
// since it is what decides which account a PIN opens). It contains only salted PIN HASHES, never
// PINs or keys, so reading it does not reveal a PIN or open an account , a reader still has to brute
// force a hash, which is exactly the work the TPM lockout (on the real key) is there to punish.

const registryMagic = "GREG1"

// Save writes the registry to path atomically (write temp, rename), preserving all entries.
func (r *Registry) Save(path string) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)

	if _, err := w.WriteString(registryMagic); err != nil {
		f.Close()
		return err
	}
	// salt length + salt
	if err := binary.Write(w, binary.BigEndian, uint16(len(r.salt))); err != nil {
		f.Close()
		return err
	}
	if _, err := w.Write(r.salt); err != nil {
		f.Close()
		return err
	}
	// every entry, fixed order
	for i := range r.entries {
		e := r.entries[i]
		if len(e.hash) != hashLen {
			f.Close()
			return fmt.Errorf("entry %d has bad hash length %d", i, len(e.hash))
		}
		if _, err := w.Write(e.hash); err != nil {
			f.Close()
			return err
		}
		if err := binary.Write(w, binary.BigEndian, int32(e.open)); err != nil {
			f.Close()
			return err
		}
		if err := binary.Write(w, binary.BigEndian, int32(e.wipe)); err != nil {
			f.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadRegistry reads a registry written by Save. The real-entry count (used) is recovered as the
// number of entries that are not pure random filler , filler always has open == NoSlot AND
// wipe == NoSlot , which is all `add` and `Resolve` need (Resolve scans every entry regardless).
func LoadRegistry(path string) (*Registry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	br := bufio.NewReader(f)

	magic := make([]byte, len(registryMagic))
	if _, err := readFull(br, magic); err != nil {
		return nil, err
	}
	if string(magic) != registryMagic {
		return nil, fmt.Errorf("not a registry file")
	}
	var saltLen uint16
	if err := binary.Read(br, binary.BigEndian, &saltLen); err != nil {
		return nil, err
	}
	salt := make([]byte, saltLen)
	if _, err := readFull(br, salt); err != nil {
		return nil, err
	}

	r := &Registry{salt: salt}
	used := 0
	for i := range r.entries {
		h := make([]byte, hashLen)
		if _, err := readFull(br, h); err != nil {
			return nil, err
		}
		var open, wipe int32
		if err := binary.Read(br, binary.BigEndian, &open); err != nil {
			return nil, err
		}
		if err := binary.Read(br, binary.BigEndian, &wipe); err != nil {
			return nil, err
		}
		r.entries[i] = entry{hash: h, open: int(open), wipe: int(wipe)}
		if int(open) != NoSlot || int(wipe) != NoSlot {
			used = i + 1 // real entries are packed at the front by add()
		}
	}
	r.used = used
	return r, nil
}

func readFull(r *bufio.Reader, b []byte) (int, error) {
	got := 0
	for got < len(b) {
		n, err := r.Read(b[got:])
		got += n
		if err != nil {
			return got, err
		}
	}
	return got, nil
}
