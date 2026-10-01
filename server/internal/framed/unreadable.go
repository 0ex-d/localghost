package framed

// THE PHOTOS NOTHING ON THE BOX CAN READ. A photo whose original Go's decoder and ffmpeg both
// refuse is marked (frames.unreadable) and set apart: no preview, no caption, not re-read at every
// start. What the box can still say about one is whether the damage came with it: the file's
// bytes still hash to its name (the archive is named by the hash of what arrived, so an intact
// file is exactly what the phone sent, damaged before it left the phone), how it starts, and the
// longest run of zero bytes in it (a copy cut short and padded, or a cloud placeholder half
// fetched, leaves a block of zeros where the picture was). `ghost-cli ghost.framed unreadable`.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/hw"
)

// UnreadableCheck is one unreadable photo with what its file says.
type UnreadableCheck struct {
	hw.UnreadableFrame
	Taken    string `json:"taken"`              // the capture time, readable
	OnDisk   int64  `json:"onDisk"`             // the file's size now (-1 when it is missing)
	Intact   bool   `json:"intact"`             // its bytes still hash to its name: it arrived like this
	Starts   string `json:"starts"`             // what its first bytes say it is
	Head     string `json:"head"`               // the first 12 bytes, hex (FFD8FF… is a JPEG's start)
	ZeroRun  int64  `json:"zeroRun"`            // the longest run of zero bytes
	ZeroAt   int64  `json:"zeroAt,omitempty"`   // where it starts
	ZeroTail int64  `json:"zeroTail,omitempty"` // zero bytes at the very end
}

// CheckUnreadable reads each unreadable photo's file (a few MB each, a handful of photos).
func (p *Pipeline) CheckUnreadable(limit int) ([]UnreadableCheck, error) {
	list, err := p.store.Unreadable(limit)
	if err != nil {
		return nil, err
	}
	out := make([]UnreadableCheck, 0, len(list))
	for _, u := range list {
		c := UnreadableCheck{UnreadableFrame: u, OnDisk: -1, Taken: time.Unix(u.TakenAt, 0).UTC().Format("2006-01-02 15:04 UTC") + " (" + u.TakenSrc + ")"}
		if raw, rerr := os.ReadFile(u.ArchivePath); rerr == nil {
			c.OnDisk = int64(len(raw))
			sum := sha256.Sum256(raw)
			c.Intact = hex.EncodeToString(sum[:16]) == u.Hash
			head := raw
			if len(head) > 12 {
				head = head[:12]
			}
			c.Head = hex.EncodeToString(head)
			c.Starts = startsAs(raw)
			c.ZeroRun, c.ZeroAt = longestZeroRun(raw)
			c.ZeroTail = int64(len(raw) - len(bytes.TrimRight(raw, "\x00")))
		}
		out = append(out, c)
	}
	return out, nil
}

// RetryUnreadable reads every unreadable photo again with today's decoders (after an ffmpeg
// upgrade, say): a preview made clears the mark and the photo goes on to its caption.
func (p *Pipeline) RetryUnreadable(limit int) (tried, readable int) {
	list, err := p.store.Unreadable(limit)
	if err != nil {
		return 0, 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, u := range list {
		tried++
		f, prev, derr := p.derive(u.ArchivePath, true)
		if derr != nil || !prev {
			continue
		}
		readable++
		if p.notifySearch != nil {
			p.notifySearch(f.ArchivePath, renderFor(f.Kind, f.ArchivePath, f.PreviewPath), f.TakenAt, true)
		}
	}
	return tried, readable
}

// startsAs names a file by its first bytes.
func startsAs(b []byte) string {
	switch {
	case len(b) == 0:
		return "empty"
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}):
		return "a JPEG's start"
	case bytes.HasPrefix(b, []byte{0x89, 'P', 'N', 'G'}):
		return "a PNG's start"
	case len(b) >= 12 && string(b[4:8]) == "ftyp":
		return "an ISO media file (" + string(b[8:12]) + ")"
	case bytes.HasPrefix(b, []byte("RIFF")):
		return "a RIFF file (WebP?)"
	case b[0] == 0:
		return "zeros"
	}
	return "nothing recognisable"
}

// longestZeroRun is the longest run of 0x00 bytes and where it starts.
func longestZeroRun(b []byte) (n, at int64) {
	var cur, start int64
	for i, x := range b {
		if x == 0 {
			if cur == 0 {
				start = int64(i)
			}
			cur++
			if cur > n {
				n, at = cur, start
			}
		} else {
			cur = 0
		}
	}
	return n, at
}
