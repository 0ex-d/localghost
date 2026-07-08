package search

// Ingest (spec 4, 9.1): tombstone check, dedup insert, chunk, enqueue. The write path never waits for
// a GPU , embedding and captioning are jobs. No cross-call transactions exist in poltergres (deliberate),
// so the original-then-chunks pair is made crash-safe by healing instead: a duplicate insert returns
// the existing id, and an original with zero chunks gets re-chunked. Same outcome as the spec's
// transaction, one mechanism instead of two.

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	_ "image/jpeg" // decoders for pHash
	_ "image/png"
	"log/slog"
	"os"
)

type Ingester struct {
	Store *Store
	Log   *slog.Logger
}

// IngestText ingests a text-bearing original (email/message/document/audio-transcript). body is the
// extracted text; header is the context header (spec 5). Returns the original id.
func (in *Ingester) IngestText(o Original, header, body string) (int64, error) {
	sha := sha256.Sum256([]byte(body))
	if o.SHA256 == nil {
		o.SHA256 = sha[:]
	}
	if dead, err := in.Store.Tombstoned(o.SHA256); err != nil {
		return 0, err
	} else if dead {
		return 0, fmt.Errorf("refused: content is tombstoned (deleted content never returns)")
	}
	id, existed, err := in.Store.InsertOriginal(o)
	if err != nil {
		return 0, err
	}
	if existed {
		if n, err := in.Store.ChunkCount(0, o.Source, id); err == nil && n > 0 {
			return id, nil // fully ingested before; dedup stop (spec 4 step 3)
		}
		// fall through: heal the half-done ingest
	}
	if o.Source == "email" {
		body = StripQuotedEmail(body)
	}
	chunks := ChunkText(header, body)
	if len(chunks) == maxChunks {
		in.Log.Warn("chunk cap hit, truncated", "fn", "IngestText", "source", o.Source, "id", id)
	}
	ids, err := in.Store.InsertChunksT0(o.Source, id, o.CapturedAt, chunks)
	if err != nil {
		return id, err
	}
	if err := in.enqueueEmbeds(ids); err != nil {
		return id, err
	}
	return id, nil
}

// IngestImage ingests an image (spec 9.1): pHash burst-collapse, meta, caption job. The caption
// arrives later and creates the chunks; the image is findable by filters immediately and by text once
// captioned.
func (in *Ingester) IngestImage(o Original, imageBytes []byte) (int64, error) {
	sha := sha256.Sum256(imageBytes)
	if o.SHA256 == nil {
		o.SHA256 = sha[:]
	}
	if dead, err := in.Store.Tombstoned(o.SHA256); err != nil {
		return 0, err
	} else if dead {
		return 0, fmt.Errorf("refused: content is tombstoned")
	}
	id, existed, err := in.Store.InsertOriginal(o)
	if err != nil {
		return 0, err
	}
	if existed {
		return id, nil // image dedup is exact-hash; near-dup handled below for new rows only
	}
	img, _, derr := image.Decode(bytes.NewReader(imageBytes))
	if derr != nil {
		in.Log.Warn("image undecodable, ingested without phash/caption", "fn", "IngestImage", "id", id, "err", derr)
		return id, nil
	}
	ph := DHash(img)
	if err := in.Store.SetPhash(id, ph); err != nil {
		return id, err
	}
	if repID, dist, found, err := in.Store.NearestPhash(ph, id); err == nil && found && dist <= 6 {
		// Burst sibling (spec 9.1 step 2): mark and skip captioning; reachable via its representative.
		in.Log.Info("burst sibling, caption skipped", "fn", "IngestImage", "id", id, "dupOf", repID, "hamming", dist)
		return id, in.Store.db.Exec(
			`UPDATE search.originals SET meta = meta || jsonb_build_object('dup_of', $1::bigint)
			 WHERE source = 'image' AND id = $2`, repID, id)
	}
	return id, in.Store.EnqueueJob("caption", map[string]any{"origId": id, "path": o.Path})
}

func (in *Ingester) enqueueEmbeds(chunkIDs []int64) error {
	const batch = 64 // spec 4: claim up to 64 chunk ids per embed call
	for start := 0; start < len(chunkIDs); start += batch {
		end := start + batch
		if end > len(chunkIDs) {
			end = len(chunkIDs)
		}
		if err := in.Store.EnqueueJob("embed_text", map[string]any{"chunkIds": chunkIDs[start:end]}); err != nil {
			return err
		}
	}
	return nil
}

// Rebuild (spec 13.3): truncate derived chunks, re-chunk every original from its source-appropriate
// text, re-enqueue embeds. Images re-chunk from meta.caption , no re-captioning (that is a model
// migration, a different, expensive operation). Proof that indexes are derived state.
func (in *Ingester) Rebuild() (int, error) {
	if err := in.Store.RebuildTruncate(); err != nil {
		return 0, err
	}
	origs, err := in.Store.AllOriginals()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, o := range origs {
		var body string
		switch o.Source {
		case "image":
			if c, ok := o.Meta["caption"].(string); ok {
				body = c
			}
		default:
			b, rerr := os.ReadFile(o.Path)
			if rerr != nil {
				in.Log.Warn("rebuild: original unreadable, skipped", "fn", "Rebuild",
					"source", o.Source, "id", o.ID, "err", rerr)
				continue
			}
			body = string(b)
		}
		if body == "" {
			continue
		}
		header := ContextHeader(o.Source, o.CapturedAt.Format("2006-01-02"))
		chunks := ChunkText(header, body)
		ids, cerr := in.Store.InsertChunksT0(o.Source, o.ID, o.CapturedAt, chunks)
		if cerr != nil {
			return n, cerr
		}
		if err := in.enqueueEmbeds(ids); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
