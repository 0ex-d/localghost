package framed

import (
	"bytes"
	"encoding/binary"
)

// MediaKind is what a spooled file actually is, decided by its CONTENT, not its name. Uploads arrive
// as <nanos>-<rand> with no extension (secd deliberately never inspects bytes), so framed sniffs the
// magic bytes here. This is authoritative: a phone that mislabels, or a future client that sends no
// hint at all, is still classified correctly.
type MediaKind int

const (
	KindUnknown MediaKind = iota
	KindPhoto
	KindVideo
)

// SniffResult is the detected type plus the canonical extension to archive it under.
type SniffResult struct {
	Kind MediaKind
	Ext  string // includes the dot, e.g. ".jpg", ".mp4"; ".bin" when truly unknown
	MIME string // best-effort, for the record; "" when unknown
}

// Sniff identifies a media file from its leading bytes. It recognises the formats a phone camera
// actually produces , JPEG, PNG, HEIF/HEIC/AVIF, WebP, GIF for stills; MP4, QuickTime MOV, and the
// common ISO-BMFF brands plus WebM/Matroska for video , and falls back to KindUnknown/.bin for
// anything else so nothing is ever silently mis-archived.
func Sniff(b []byte) SniffResult {
	if len(b) < 12 {
		return SniffResult{KindUnknown, ".bin", ""}
	}

	// --- stills ---
	switch {
	case b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF: // JPEG SOI
		return SniffResult{KindPhoto, ".jpg", "image/jpeg"}
	case bytes.HasPrefix(b, []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return SniffResult{KindPhoto, ".png", "image/png"}
	case bytes.HasPrefix(b, []byte("GIF87a")) || bytes.HasPrefix(b, []byte("GIF89a")):
		return SniffResult{KindPhoto, ".gif", "image/gif"}
	case bytes.Equal(b[0:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return SniffResult{KindPhoto, ".webp", "image/webp"}
	}

	// --- ISO base media file format (MP4/MOV/3GP and HEIF/AVIF share the ftyp box) ---
	if bytes.Equal(b[4:8], []byte("ftyp")) {
		return sniffISO(b)
	}

	// --- Matroska / WebM (EBML header) ---
	if bytes.HasPrefix(b, []byte{0x1A, 0x45, 0xDF, 0xA3}) {
		// Could be .mkv or .webm; .webm is the phone-relevant one. Default to .webm.
		return SniffResult{KindVideo, ".webm", "video/webm"}
	}

	// --- 3GPP (older phone video) also uses ftyp, caught above; AVI as a fallback ---
	if bytes.Equal(b[0:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("AVI ")) {
		return SniffResult{KindVideo, ".avi", "video/x-msvideo"}
	}

	return SniffResult{KindUnknown, ".bin", ""}
}

// heifBrands are the ISO-BMFF brands that name a STILL: the HEIF image brands (ISO 23008-12; every
// HEIF image file carries mif1 among its compatible brands, whatever its major brand says), the
// MIAF profile brands, and AVIF. A clip's brands are isom, mp41, mp42, avc1, qt, 3gp and kin, and a
// clip never carries an image brand.
var heifBrands = map[string]bool{
	"mif1": true, "msf1": true, "miaf": true, "jpeg": true,
	"heic": true, "heix": true, "hevc": true, "hevx": true,
	"heim": true, "heis": true, "hevm": true, "hevs": true,
	"MiHE": true, "MiPr": true, "MiHB": true, "MiAC": true, "MiAB": true,
	"avif": true, "avis": true,
}

// sniffISO tells a still from a clip inside the ISO base media format. The major brand alone is not
// enough: a phone's HEIF photo can carry a major brand outside the familiar few (hevx for a 10-bit
// or HDR still, heif on some cameras, mif1 with the real brand among the compatible ones), and
// reading only the major brand filed such photos as MP4 videos, with a play glyph on every
// thumbnail of a day. So every brand the ftyp box lists is read; an image brand anywhere makes it a
// still. A box with no brand to decide by is told by what follows ftyp: a HEIF puts its meta box
// there, a clip its moov or mdat.
func sniffISO(b []byte) SniffResult {
	size := int(binary.BigEndian.Uint32(b[0:4]))
	if size < 16 || size > len(b) {
		size = len(b)
		if size > 256 {
			size = 256
		}
	}
	major := string(b[8:12])
	brands := []string{major}
	for i := 16; i+4 <= size; i += 4 {
		brands = append(brands, string(b[i:i+4]))
	}
	still, avif := false, false
	for _, br := range brands {
		if heifBrands[br] {
			still = true
		}
		if br == "avif" || br == "avis" {
			avif = true
		}
	}
	if !still && size+8 <= len(b) && string(b[size+4:size+8]) == "meta" {
		still = true // no brand named it, but it is built like a HEIF: ftyp, then the item meta
	}
	switch {
	case still && avif:
		return SniffResult{KindPhoto, ".avif", "image/avif"}
	case still:
		return SniffResult{KindPhoto, ".heic", "image/heic"}
	case major == "qt  ":
		return SniffResult{KindVideo, ".mov", "video/quicktime"}
	default:
		// isom, mp41, mp42, iso2, iso5, M4V, 3gp, dash, etc. , an MP4 video
		return SniffResult{KindVideo, ".mp4", "video/mp4"}
	}
}
