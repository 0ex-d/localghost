package search

// Worker loop (spec 4): claim -> do -> complete/fail-with-backoff. Polling, not LISTEN/NOTIFY , the
// in-house pg client has no async notification path yet (D2), and at single-user scale a poll tick
// costs nothing measurable. The interval is conf. embed concurrency is capped at ONE in-flight batch
// (spec: embed_max_concurrent_batches default 1) so interactive inference keeps the hardware.

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

type Worker struct {
	Store    *Store
	Embed    *Embedder // nil = vector-less; embed jobs are not claimed
	Caption  Captioner
	Ingester *Ingester
	Log      *slog.Logger
	Interval time.Duration
}

// Run polls all job kinds until ctx ends.
func (w *Worker) Run(ctx context.Context) {
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	// Drain greedily per tick but one job at a time per kind , the single-conn store serialises anyway.
	if w.Embed != nil {
		for w.one(ctx, "embed_text", w.doEmbed) {
		}
	}
	for w.one(ctx, "caption", w.doCaption) {
	}
	for w.one(ctx, "reconsolidate", w.doReconsolidate) {
	}
}

func (w *Worker) one(ctx context.Context, kind string, do func(context.Context, *Job) error) bool {
	if ctx.Err() != nil {
		return false
	}
	job, err := w.Store.ClaimJob(kind)
	if err != nil || job == nil {
		return false
	}
	if err := do(ctx, job); err != nil {
		w.Log.Warn("job failed", "fn", "one", "kind", kind, "job", job.ID, "err", err)
		_ = w.Store.FailJob(job.ID, err)
		return true
	}
	_ = w.Store.CompleteJob(job.ID)
	return true
}

func (w *Worker) doEmbed(ctx context.Context, job *Job) error {
	var p struct {
		ChunkIDs []int64 `json:"chunkIds"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return err
	}
	bodies, err := w.Store.ChunkBodies(p.ChunkIDs)
	if err != nil {
		return err
	}
	ids := make([]int64, 0, len(bodies))
	texts := make([]string, 0, len(bodies))
	for _, id := range p.ChunkIDs {
		if b, ok := bodies[id]; ok {
			ids = append(ids, id)
			texts = append(texts, b)
		}
	}
	if len(texts) == 0 {
		return nil // chunks deleted since enqueue; done
	}
	vecs, err := w.Embed.Embed(ctx, texts)
	if err != nil {
		return err
	}
	for i, id := range ids {
		if err := w.Store.SetEmbedding(id, vecs[i], w.Embed.ModelID); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) doCaption(ctx context.Context, job *Job) error {
	var p struct {
		OrigID int64  `json:"origId"`
		Path   string `json:"path"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return err
	}
	caption, err := w.Caption.Caption(ctx, p.Path)
	if err != nil {
		return err // ErrNoVision parks here, visibly, until oracled can see
	}
	// Store the caption in meta AND chunk it (spec 9.1 steps 6-7).
	if err := w.Store.db.Exec(
		`UPDATE search.originals SET meta = meta || jsonb_build_object('caption', $1::text)
		 WHERE source = 'image' AND id = $2`, caption, p.OrigID); err != nil {
		return err
	}
	_, sha, meta, captured, err := w.Store.OriginalByID("image", p.OrigID)
	_ = sha
	if err != nil {
		return err
	}
	header := ContextHeader("photo", captured.Format("2006-01-02"), metaCamera(meta))
	chunks := ChunkText(header, caption)
	ids, err := w.Store.InsertChunksT0("image", p.OrigID, captured, chunks)
	if err != nil {
		return err
	}
	return w.Ingester.enqueueEmbeds(ids)
}

// doReconsolidate is spec 12 phase 2. The real regeneration belongs to the consolidation daemon
// (T1/T2 producer), which does not exist yet; until it does, the honest phase 2 for a row with zero
// surviving sources is deletion, and a row WITH survivors stays stale (excluded from every search
// path) rather than being un-staled with content that still cites deleted material. Stale-forever is
// the safe failure; un-staling without regeneration would be the privacy failure.
func (w *Worker) doReconsolidate(_ context.Context, job *Job) error {
	var p struct {
		Tier  int   `json:"tier"`
		RefID int64 `json:"refId"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return err
	}
	col := "entry_id"
	if p.Tier == 2 {
		col = "memory_id"
	}
	rows, err := w.Store.db.Query(
		"SELECT count(*) FROM search.citations WHERE tier = $1 AND ref_id = $2", p.Tier, p.RefID)
	if err != nil {
		return err
	}
	surviving := 0
	if len(rows.Vals) > 0 && rows.Vals[0][0] != nil {
		if n, perr := jsonAtoi(*rows.Vals[0][0]); perr == nil {
			surviving = n
		}
	}
	if surviving == 0 {
		return w.Store.db.Exec("DELETE FROM search.chunks WHERE tier = $1 AND "+col+" = $2", p.Tier, p.RefID)
	}
	w.Log.Info("reconsolidate deferred: row has survivors, stays stale until consolidation daemon exists",
		"fn", "doReconsolidate", "tier", p.Tier, "ref", p.RefID, "surviving", surviving)
	return nil
}

func metaCamera(meta map[string]any) string {
	if meta == nil {
		return ""
	}
	if c, ok := meta["camera"].(string); ok {
		return c
	}
	return ""
}

func jsonAtoi(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errNotNumber
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

var errNotNumber = jsonErr("not a number")

type jsonErr string

func (e jsonErr) Error() string { return string(e) }
