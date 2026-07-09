package pair

// Multi-frame enrolment QR. A real device identity (P256 cert + key, PEM-wrapped, base64url'd into
// the link) is ~1.4 KB , too much for one comfortably-scannable QR. Rather than push a single dense
// v31 symbol at the phone camera, we split the link into a few small frames the app scans in
// sequence ("Scan 1 of 3"). Each frame is its own modest QR (~v12), which a phone reads instantly.
//
// Frame wire format, one line, ASCII:
//
//	LGQR1 <seq> <total> <sha8> <chunk>
//
//	LGQR1  literal magic + format version 1 (bump if this framing changes)
//	seq    1-based frame index
//	total  frame count
//	sha8   first 8 hex chars of SHA-256 over the FULL reassembled link , the app uses it to (a) know
//	       all frames belong to the same enrolment and (b) verify the join before parsing
//	chunk  a slice of the raw link string (NOT re-encoded; the link is already URL-safe ASCII)
//
// The app collects frames until it has all `total`, concatenates by `seq`, checks sha8, then parses
// the result exactly as it parses a single-QR link. Order-independent and duplicate-safe: rescanning
// a frame is idempotent, and the count is known from frame one.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// ChunkMagic identifies a multi-frame enrolment payload and pins the framing version.
const ChunkMagic = "LGQR1"

// chunkPayloadBytes is the max link-slice bytes per frame. Chosen so a frame (header + chunk) lands
// around QR v12 at EC level M (~290 data bytes), an easy phone scan. The header is ~25 bytes.
const chunkPayloadBytes = 240

// ChunkLink splits a link into ordered frame strings. A link that fits one frame returns a single
// frame , the app path is identical whether there is one frame or five, so there is no special case
// on either side.
func ChunkLink(link string) []string {
	sum := sha256.Sum256([]byte(link))
	sha8 := hex.EncodeToString(sum[:])[:8]

	total := (len(link) + chunkPayloadBytes - 1) / chunkPayloadBytes
	if total == 0 {
		total = 1
	}
	frames := make([]string, 0, total)
	for seq := 1; seq <= total; seq++ {
		start := (seq - 1) * chunkPayloadBytes
		end := start + chunkPayloadBytes
		if end > len(link) {
			end = len(link)
		}
		frames = append(frames, fmt.Sprintf("%s %d %d %s %s", ChunkMagic, seq, total, sha8, link[start:end]))
	}
	return frames
}

// JoinFrames reassembles frames into the original link (the box-side inverse, and a spec the app
// mirrors). It tolerates any order and duplicate frames; it rejects mixed sets, gaps, and a bad
// checksum , the failures a real scan session actually produces.
func JoinFrames(frames []string) (string, error) {
	if len(frames) == 0 {
		return "", fmt.Errorf("no frames")
	}
	var total int
	var sha8 string
	parts := map[int]string{}
	for _, f := range frames {
		fields := strings.SplitN(f, " ", 5)
		if len(fields) != 5 || fields[0] != ChunkMagic {
			return "", fmt.Errorf("not a %s frame", ChunkMagic)
		}
		seq, err := strconv.Atoi(fields[1])
		if err != nil || seq < 1 {
			return "", fmt.Errorf("bad seq %q", fields[1])
		}
		tot, err := strconv.Atoi(fields[2])
		if err != nil || tot < 1 {
			return "", fmt.Errorf("bad total %q", fields[2])
		}
		if total == 0 {
			total, sha8 = tot, fields[3]
		} else if tot != total || fields[3] != sha8 {
			return "", fmt.Errorf("frame from a different enrolment set")
		}
		if seq > total {
			return "", fmt.Errorf("seq %d exceeds total %d", seq, total)
		}
		parts[seq] = fields[4]
	}
	var b strings.Builder
	for seq := 1; seq <= total; seq++ {
		p, ok := parts[seq]
		if !ok {
			return "", fmt.Errorf("missing frame %d of %d", seq, total)
		}
		b.WriteString(p)
	}
	link := b.String()
	sum := sha256.Sum256([]byte(link))
	if hex.EncodeToString(sum[:])[:8] != sha8 {
		return "", fmt.Errorf("checksum mismatch , frames do not reassemble to a valid link")
	}
	return link, nil
}
