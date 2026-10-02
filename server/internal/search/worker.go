package search

// Worker loop (spec 4): claim -> do -> complete/fail-with-backoff. Polling, not LISTEN/NOTIFY , the
// in-house pg client has no async notification path yet (D2), and at single-user scale a poll tick
// costs nothing measurable. The interval is conf. embed concurrency is capped at ONE in-flight batch
// (spec: embed_max_concurrent_batches default 1) so interactive inference keeps the hardware.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/workcount"
)

type Worker struct {
	holdMu     sync.Mutex
	modelHold  laneHold // every model lane (caption, tag, categorize) rests: oracled warming, or a chat
	visionHold laneHold // only captions rest: the engine is up but sees no images; text work goes on
	Store      *Store
	Embed      *Embedder // nil = vector-less; embed jobs are not claimed
	Caption    Captioner
	Tag        Tagger // nil = tags parked like vision-less captions
	Ingester   *Ingester
	Log        *slog.Logger
	Interval   time.Duration
	// Work counts what the worker did, by job kind (and "<kind> failed"), for the drill-in; nil
	// counts nothing
	Work *workcount.Counter
}

// laneHold is a lane resting: until when, why, and since when (one spell, across the short holds
// that keep renewing it).
type laneHold struct {
	until, since time.Time
	why          string
}

// set rests the lane for d; fresh when it was not resting (the caller logs one line per storm).
func (h *laneHold) set(d time.Duration, why string) (fresh bool) {
	fresh = time.Now().After(h.until)
	// one resting spell, for `queue`, until the lane has run for two minutes without a hold
	if time.Since(h.until) > 2*time.Minute {
		h.since = time.Now()
	}
	h.until, h.why = time.Now().Add(d), why
	return fresh
}

func (h *laneHold) on() bool { return time.Now().Before(h.until) }

func (h *laneHold) state() LaneState {
	if !h.on() {
		return LaneState{}
	}
	return LaneState{Resting: true, Until: h.until.Unix(), Why: h.why, RestingSince: h.since.Unix()}
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

// categorizePerRound bounds the category backfill between two captions (see tick).
const categorizePerRound = 3

// RunOnce is one tick: every lane worked until nothing is runnable (or the model lanes rest).
func (w *Worker) RunOnce(ctx context.Context) { w.tick(ctx) }

// held: every model lane rests.
func (w *Worker) held() bool {
	w.holdMu.Lock()
	defer w.holdMu.Unlock()
	return w.modelHold.on()
}

// captionsHeld: the caption lane rests (with the rest, or alone for want of vision).
func (w *Worker) captionsHeld() bool {
	w.holdMu.Lock()
	defer w.holdMu.Unlock()
	return w.modelHold.on() || w.visionHold.on()
}

// LaneState is whether a lane is resting, until when and why: the one thing the job counts cannot
// show, a queue that is full because nothing is taking from it.
type LaneState struct {
	Resting      bool   `json:"resting"`
	Until        int64  `json:"until,omitempty"` // unix seconds
	Why          string `json:"why,omitempty"`
	RestingSince int64  `json:"restingSince,omitempty"`
}

// Lanes is the model lanes' hold (all of caption, tag and categorize).
func (w *Worker) Lanes() LaneState {
	w.holdMu.Lock()
	defer w.holdMu.Unlock()
	return w.modelHold.state()
}

// CaptionLane is the caption lane's own hold: the engine takes no images.
func (w *Worker) CaptionLane() LaneState {
	w.holdMu.Lock()
	defer w.holdMu.Unlock()
	return w.visionHold.state()
}

// tick works the lanes in ROUNDS until a round finds nothing to do. It used to drain each lane in
// turn, captions first: with a backlog of captions (thousands, after an unpark or a restore) the
// tag pass waited behind all of them, so photos got a description and nothing else for days,
// untitled and untagged, and the embeds for their chunks waited too. A round now is: the embeds,
// every tag pass that is runnable (text only, seconds each, and each one names a photo that is
// already described), a few of the category backfill, then ONE caption. So a caption's tag pass
// runs in the round after it, newest photos first, and a photo that arrives while the backlog
// runs is described, named and tagged within a couple of rounds.
func (w *Worker) tick(ctx context.Context) {
	for ctx.Err() == nil {
		did := false
		if w.Embed != nil {
			for w.one(ctx, "embed_text", w.doEmbed) {
				did = true
			}
		}
		// MODEL GATE , while oracled is warming (llama loading 7GB), model-dependent lanes REST
		// instead of machine-gunning fast-fails through the queue. The first "no backend" sets the
		// hold; nothing model-bound runs until it lapses. Embeds are unaffected (next tick).
		if w.held() {
			return
		}
		for !w.held() && w.one(ctx, "tag", w.doTags) {
			did = true
		}
		for n := 0; n < categorizePerRound && !w.held() && w.one(ctx, "categorize", w.doCategorize); n++ {
			did = true
		}
		// a model that sees no images holds only this lane: tags and categories are text
		if !w.captionsHeld() && w.one(ctx, "caption", w.doCaption) {
			did = true
		}
		for w.one(ctx, "reconsolidate", w.doReconsolidate) {
			did = true
		}
		if !did {
			return
		}
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
		if strings.Contains(err.Error(), "no vision:") {
			// The model cannot take images at all (llama-server up without its projector): every
			// caption would fail the same way. Hold the lane and keep the job's attempts, so fixing
			// the server resumes the queue instead of finding five thousand parked jobs.
			_ = w.Store.UnclaimJob(job.ID)
			w.holdMu.Lock()
			fresh := w.visionHold.set(5*time.Minute, err.Error())
			w.holdMu.Unlock()
			if fresh {
				w.Log.Warn("the model takes no images , caption lane held 5 min, jobs kept (tags and categories go on)", "fn", "one", "why", err.Error())
			}
			return false
		}
		if strings.Contains(err.Error(), "no backend") || strings.Contains(err.Error(), "preempted") {
			// Oracled is warming, or it set this job aside because a person started a chat , refund
			// the attempt (the job did nothing wrong) and hold the model lanes. One log line per
			// storm, not one per job.
			_ = w.Store.UnclaimJob(job.ID)
			w.holdMu.Lock()
			fresh := w.modelHold.set(20*time.Second, err.Error())
			w.holdMu.Unlock()
			if fresh {
				w.Log.Info("model warming or in a chat , caption/tag lanes resting 20s", "fn", "one", "why", err.Error())
			}
			return false
		}
		if strings.Contains(err.Error(), "too large to decode") {
			// past the box's image limits (imgfit/limits.go): no attempt will ever read it, so the
			// job is done without a caption instead of failing five times and parking, where an
			// unpark would only start it over. The frame keeps its date, place and file.
			w.Log.Info("image past the size limits, left without a caption", "fn", "one", "kind", kind, "job", job.ID, "why", err.Error())
			_ = w.Store.CompleteJob(job.ID)
			w.Work.Add(kind+" too large", 1)
			return true
		}
		if strings.Contains(err.Error(), "the file looks damaged") {
			// nothing on the box can read the picture (oracled tried Go's decoder and ffmpeg): no
			// attempt will ever caption it, so the job is done without one, like a picture past
			// the size limits, instead of failing five times and coming back at every stock-take.
			// framed moves the photo to frames/damaged and Box Status counts it there.
			w.Log.Info("image unreadable (damaged), left without a caption", "fn", "one", "kind", kind, "job", job.ID)
			_ = w.Store.CompleteJob(job.ID)
			w.Work.Add(kind+" unreadable", 1)
			return true
		}
		w.Log.Warn("job failed", "fn", "one", "kind", kind, "job", job.ID, "err", err)
		_ = w.Store.FailJob(job.ID, err)
		w.Work.Add(kind+" failed", 1)
		return true
	}
	_ = w.Store.CompleteJob(job.ID)
	w.Work.Add(kind, 1)
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
	vecs, cut, err := w.Embed.EmbedFitting(ctx, texts)
	if err != nil {
		return err
	}
	if cut > 0 {
		w.Log.Info("embedded from the beginning of chunks too long for the embedding model", "fn", "doEmbed", "job", job.ID, "chunks", cut)
	}
	w.Work.Add("chunks embedded", len(ids))
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
	// Store the caption in meta, then everything that follows a caption (spec 9.1 steps 6-7):
	// the SCENE onto frames.description, the chunks, the tag pass. One idempotent function shared
	// with the ensure path, so a re-run never doubles a chunk or overwrites a person's edit. The
	// frame's identity is the 32-hex prefix in the render's filename (archive, preview and thumb
	// all carry it); this used to bind the raw sha bytes and matched nothing, silently.
	if err := w.Store.SetCaption(p.OrigID, caption); err != nil {
		return err
	}
	_, _, _, captured, err := w.Store.OriginalByID("image", p.OrigID)
	if err != nil {
		return err
	}
	return w.Ingester.ApplyCaption(p.OrigID, p.Path, caption, captured)
}

// doTags turns a caption into tag rows and a derived display name. The frame's identity (the 32-hex
// content hash) comes from the ARCHIVE FILENAME , framed names archived files <hash>.<ext>, so the
// path the ingest already carries is the bridge, and no schema or API grew for this.
func (w *Worker) doTags(ctx context.Context, job *Job) error {
	var p struct {
		OrigID   int64  `json:"origId"`
		Path     string `json:"path"`
		Caption  string `json:"caption"`
		Captured int64  `json:"captured"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return err
	}
	if w.Tag == nil {
		return ErrNoVision // parks visibly, same contract as caption-without-vision
	}
	tags, err := w.Tag.Tags(ctx, p.Caption)
	if err != nil {
		return err
	}
	hash := frameHashFromPath(p.Path)
	if len(tags) == 0 {
		// Nothing to tag with; the frame still gets its date as a title so a person sees SOMETHING,
		// and the stock-take keeps counting it untagged (a later pass may do better).
		w.Log.Info("no tags extracted", "fn", "doTags", "origId", p.OrigID)
		if hash == "" {
			return nil
		}
		return w.Store.db.Exec(
			`UPDATE frames SET display_name = $1 WHERE hash = $2 AND (display_name IS NULL OR display_name = '')`,
			time.Unix(p.Captured, 0).UTC().Format("2006-01-02"), hash)
	}
	if hash == "" {
		w.Log.Warn("tag pass: no frame hash in path, tags kept for search only", "fn", "doTags", "path", p.Path)
	} else {
		for _, tag := range tags {
			// User corrections outrank the model FOREVER: a tag the user removed is a tombstone row
			// (source 'user_removed'), and this NOT EXISTS keeps the model from resurrecting it.
			// The category rides in from the tag pass (or the lexicon); '' means a later
			// categorize job fills it.
			if err := w.Store.db.Exec(
				`INSERT INTO frame_tags (hash, tag, source, created_at, category)
				 SELECT $1, $2, 'model', $3, $4
				 WHERE NOT EXISTS (SELECT 1 FROM frame_tags WHERE hash = $1 AND tag = $2)`,
				hash, tag.Name, time.Now().UTC().Unix(), tag.Category); err != nil {
				return err
			}
		}
		// Derived display name: date + the first three tags. Set only when EMPTY , a user rename
		// (future endpoint) must never be overwritten by a background job.
		name := time.Unix(p.Captured, 0).UTC().Format("2006-01-02")
		n := 2 // SHORT: tags, description and place carry the detail; the name is a label
		if len(tags) < n {
			n = len(tags)
		}
		for _, t := range tags[:n] {
			name += " " + t.Name
		}
		if err := w.Store.db.Exec(
			`UPDATE frames SET display_name = $1 WHERE hash = $2 AND (display_name IS NULL OR display_name = '')`,
			name, hash); err != nil {
			return err
		}
	}
	// Tags into the search surface too , one extra chunk makes every tag retrievable. Once: a
	// tag pass re-run by the stock-take (title was missing, say) must not stack a second copy.
	if has, herr := w.Store.HasTagChunk(p.OrigID); herr == nil && has {
		return nil
	}
	captured := time.Unix(p.Captured, 0).UTC()
	ids, err := w.Store.InsertChunksT0("image", p.OrigID, captured, ChunkText("", "tags: "+strings.Join(TagNames(tags), ", ")))
	if err != nil {
		return err
	}
	return w.Ingester.enqueueEmbeds(ids)
}

// doCategorize is the backfill for tags written before categories existed, and for whatever the
// tag pass left at ”. Lexicon first (free); the model only for the remainder. A tag the model
// places nowhere gets OtherCategory, not a guess: before, it stayed empty and the frame was queued
// again at every stock-take, the same frames asked the same question forever while the rest of the
// backlog never came up (24k frames "without category" moved by ten in a night).
// Payload: {"hash": ..., "tags": [...]}.
func (w *Worker) doCategorize(ctx context.Context, job *Job) error {
	var p struct {
		Hash string   `json:"hash"`
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return err
	}
	if p.Hash == "" || len(p.Tags) == 0 {
		return nil
	}
	set := func(tag, cat string) error {
		return w.Store.db.Exec(
			`UPDATE frame_tags SET category = $3 WHERE hash = $1 AND tag = $2 AND category = ''`, p.Hash, tag, cat)
	}
	var rest []string
	for _, t := range p.Tags {
		if strings.TrimSpace(t) == "" {
			// a blank tag (a caption parse that left an empty item): nothing to ask about. Sent to
			// the model it came back "please provide the tags" and failed the whole job, forever.
			if err := set(t, OtherCategory); err != nil {
				return err
			}
			continue
		}
		if c := Lexicon(t); c != "" {
			if err := set(t, c); err != nil {
				return err
			}
		} else {
			rest = append(rest, t)
		}
	}
	if len(rest) == 0 || w.Tag == nil {
		return nil
	}
	got, err := w.Tag.Categorize(ctx, rest)
	if err != nil {
		return err
	}
	for _, t := range got {
		if err := set(t.Name, t.Category); err != nil {
			return err
		}
	}
	return nil
}

// frameHashFromPath extracts the 32-hex content hash from an archive filename (<hash>.<ext>).
func frameHashFromPath(path string) string {
	base := path
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	if len(base) != 32 {
		return ""
	}
	for _, r := range base {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return ""
		}
	}
	return base
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

// captionSection pulls one fixed heading's body out of the structured caption , the sections are a
// contract (spec 9.2), so this is a scan to the next ALL-CAPS heading, not a parser.
func captionSection(caption, heading string) string {
	i := strings.Index(caption, heading)
	if i < 0 {
		return ""
	}
	rest := caption[i+len(heading):]
	for _, next := range []string{"OBJECTS:", "PEOPLE:", "TEXT:", "COLOURS_STYLE:", "SETTING_GUESS:"} {
		if j := strings.Index(rest, next); j >= 0 {
			rest = rest[:j]
		}
	}
	out := strings.TrimSpace(rest)
	if r := []rune(out); len(r) > 900 {
		out = string(r[:900])
	}
	return out
}
