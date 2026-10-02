package framed

import (
	"encoding/binary"
	"testing"
)

// ftyp builds an ISO-BMFF head: the ftyp box with the given major and compatible brands, then one
// box of type next (empty, size 8) so the structural fallback has something to read.
func ftyp(major string, compat []string, next string) []byte {
	size := 16 + 4*len(compat)
	b := make([]byte, 0, size+8)
	b = binary.BigEndian.AppendUint32(b, uint32(size))
	b = append(b, "ftyp"...)
	b = append(b, major...)
	b = append(b, 0, 0, 0, 0) // minor version
	for _, c := range compat {
		b = append(b, c...)
	}
	b = binary.BigEndian.AppendUint32(b, 8)
	b = append(b, next...)
	return b
}

func TestSniffStillsAndClips(t *testing.T) {
	cases := []struct {
		name string
		b    []byte
		kind MediaKind
		ext  string
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE1, 0, 0, 0, 0, 0, 0, 0, 0, 0}, KindPhoto, ".jpg"},
		{"png", append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 8)...), KindPhoto, ".png"},
		{"webp", []byte("RIFF\x00\x00\x00\x00WEBPVP8 "), KindPhoto, ".webp"},
		{"heic, the usual major brand", ftyp("heic", []string{"mif1", "heic"}, "meta"), KindPhoto, ".heic"},
		{"heif with the real brand among the compatible ones", ftyp("mif1", []string{"mif1", "heic"}, "meta"), KindPhoto, ".heic"},
		{"10-bit heif (hevx major)", ftyp("hevx", []string{"mif1", "heix", "hevx"}, "meta"), KindPhoto, ".heic"},
		{"heif sequence (a motion photo)", ftyp("msf1", []string{"mif1", "msf1", "hevc"}, "meta"), KindPhoto, ".heic"},
		{"avif", ftyp("avif", []string{"avif", "mif1", "miaf"}, "meta"), KindPhoto, ".avif"},
		{"unknown brands but built like a heif", ftyp("zzzz", []string{"yyyy"}, "meta"), KindPhoto, ".heic"},
		{"mp4 from a phone", ftyp("isom", []string{"isom", "mp42", "avc1"}, "moov"), KindVideo, ".mp4"},
		{"mp4, mdat first", ftyp("mp42", []string{"isom", "mp42"}, "mdat"), KindVideo, ".mp4"},
		{"3gp", ftyp("3gp5", []string{"3gp5", "isom"}, "moov"), KindVideo, ".mp4"},
		{"quicktime", ftyp("qt  ", []string{"qt  "}, "wide"), KindVideo, ".mov"},
		{"webm", append([]byte{0x1A, 0x45, 0xDF, 0xA3}, make([]byte, 12)...), KindVideo, ".webm"},
		{"nothing known", []byte("II*\x00\x08\x00\x00\x00\x00\x00\x00\x00\x00"), KindUnknown, ".bin"},
		{"too short", []byte{0xFF, 0xD8}, KindUnknown, ".bin"},
	}
	for _, c := range cases {
		got := Sniff(c.b)
		if got.Kind != c.kind || got.Ext != c.ext {
			t.Errorf("%s: got kind %d ext %q, want kind %d ext %q", c.name, got.Kind, got.Ext, c.kind, c.ext)
		}
	}
}

// A head cut short of the ftyp box's declared size (a 12-byte read, a file truncated in the spool)
// still reads the brands it has, and never indexes past the bytes.
func TestSniffTruncatedFtyp(t *testing.T) {
	b := ftyp("hevx", []string{"mif1", "heix", "hevx"}, "meta")
	for n := 12; n <= len(b); n++ {
		got := Sniff(b[:n])
		if got.Kind == KindUnknown {
			t.Fatalf("%d bytes: unknown", n)
		}
		if n >= 20 && got.Kind != KindPhoto { // the first compatible brand (mif1) is in by 20 bytes
			t.Errorf("%d bytes: kind %d, want photo", n, got.Kind)
		}
	}
	// a size field claiming more than the head holds
	big := append([]byte{}, b...)
	binary.BigEndian.PutUint32(big[0:4], 1<<30)
	if got := Sniff(big); got.Kind != KindPhoto {
		t.Errorf("oversized ftyp: kind %d, want photo", got.Kind)
	}
	// the major brand alone, which is where the old sniff stopped, is not trusted over the list
	if got := Sniff(ftyp("isom", []string{"isom", "mif1"}, "meta")); got.Kind != KindPhoto {
		t.Errorf("isom major with mif1 compatible: kind %d, want photo (an image brand anywhere makes it a still)", got.Kind)
	}
}
