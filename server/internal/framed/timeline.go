package framed

// Google Timeline / location-history ingestion. Drop a Takeout Records.json (the classic format,
// often hundreds of MB , parsed with a STREAMING decoder, never loaded whole) or the newer
// on-device Timeline export (semanticSegments with geo: points) into <mount>/framed/gps-inbox and
// the years of GPS inside become track points, day paths, and map history. Best-effort per file:
// unparseable moves to gps-inbox/rejected; points batch in 5k inserts; days touched are rebuilt
// AFTER the whole file lands (one rebuild per day, not per point).

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// IngestTimelineDir processes every file in the gps inbox once. Returns points inserted.
func (p *Pipeline) IngestTimelineDir(mount string, lg *slog.Logger) int64 {
	inbox := filepath.Join(mount, "framed", "gps-inbox")
	rejected := filepath.Join(inbox, "rejected")
	done := filepath.Join(mount, "framed", "gps-done")
	for _, d := range []string{inbox, rejected, done} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return 0
		}
	}
	entries, err := os.ReadDir(inbox)
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		path := filepath.Join(inbox, e.Name())
		n, days, ierr := p.ingestTimelineFile(path, lg)
		total += n
		if ierr != nil {
			lg.Warn("timeline ingest failed, moved to rejected", "fn", "IngestTimelineDir", "file", e.Name(), "err", ierr)
			_ = os.Rename(path, filepath.Join(rejected, e.Name()))
			continue
		}
		_ = os.Rename(path, filepath.Join(done, e.Name()))
		lg.Info("timeline ingested", "fn", "IngestTimelineDir", "file", e.Name(), "points", n, "days", len(days))
		// Rebuild the touched days , potentially years' worth; sequential and logged, background
		// by nature since this whole loop runs off the archive path.
		i := 0
		for d := range days {
			p.RebuildDay(d)
			i++
			if i%200 == 0 {
				lg.Info("timeline day rebuild progress", "fn", "IngestTimelineDir", "done", i, "of", len(days))
			}
		}
		// One journal line for the import , the diary records that history arrived.
		if n > 0 {
			span := ""
			var minD, maxD string
			for d := range days {
				if minD == "" || d < minD {
					minD = d
				}
				if d > maxD {
					maxD = d
				}
			}
			span = minD + " to " + maxD
			_ = p.store.InsertJournal("timeline:"+e.Name(), time.Now().Unix(),
				"location history imported",
				fmt.Sprintf("Imported %d location points spanning %s from %s.", n, span, e.Name()))
		}
	}
	return total
}

// ingestTimelineFile parses one export. Two formats, detected by streaming the first meaningful
// token path: classic {"locations":[{latitudeE7,longitudeE7,timestampMs|timestamp}]} handled with a
// token-walking decoder (files are huge); newer {"semanticSegments":[{"timelinePath":[{"point":
// "geo:lat,lng","time":ISO}]}]} decoded per-segment.
func (p *Pipeline) ingestTimelineFile(path string, lg *slog.Logger) (int64, map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, nil, err
	}
	defer f.Close()
	days := map[string]bool{}
	var total int64
	batch := make([]TrackPoint, 0, 5000)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := p.store.InsertPoints("google-timeline", batch); err != nil {
			return err
		}
		total += int64(len(batch))
		batch = batch[:0]
		return nil
	}
	add := func(ts int64, lat, lon float64) error {
		if ts <= 0 || (lat == 0 && lon == 0) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
			return nil
		}
		batch = append(batch, TrackPoint{TS: ts, Lat: lat, Lon: lon})
		days[time.Unix(ts, 0).UTC().Format("2006-01-02")] = true
		if len(batch) >= 5000 {
			return flush()
		}
		return nil
	}

	dec := json.NewDecoder(f)
	// walk: expect {, then keys; on "locations" stream the array; on "semanticSegments" likewise.
	if _, err := dec.Token(); err != nil { // opening {
		return 0, nil, err
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return total, days, err
		}
		key, _ := keyTok.(string)
		switch key {
		case "locations":
			if _, err := dec.Token(); err != nil { // [
				return total, days, err
			}
			for dec.More() {
				var rec struct {
					TimestampMs string  `json:"timestampMs"`
					Timestamp   string  `json:"timestamp"`
					LatE7       int64   `json:"latitudeE7"`
					LonE7       int64   `json:"longitudeE7"`
				}
				if err := dec.Decode(&rec); err != nil {
					return total, days, err
				}
				var ts int64
				if rec.TimestampMs != "" {
					if ms, perr := strconv.ParseInt(rec.TimestampMs, 10, 64); perr == nil {
						ts = ms / 1000
					}
				} else if rec.Timestamp != "" {
					if t, perr := time.Parse(time.RFC3339, rec.Timestamp); perr == nil {
						ts = t.Unix()
					}
				}
				if err := add(ts, float64(rec.LatE7)/1e7, float64(rec.LonE7)/1e7); err != nil {
					return total, days, err
				}
			}
			if _, err := dec.Token(); err != nil { // ]
				return total, days, err
			}
		case "semanticSegments":
			if _, err := dec.Token(); err != nil {
				return total, days, err
			}
			for dec.More() {
				var seg struct {
					TimelinePath []struct {
						Point string `json:"point"`
						Time  string `json:"time"`
					} `json:"timelinePath"`
				}
				if err := dec.Decode(&seg); err != nil {
					return total, days, err
				}
				for _, tp := range seg.TimelinePath {
					geo := strings.TrimPrefix(tp.Point, "geo:")
					parts := strings.SplitN(geo, ",", 2)
					if len(parts) != 2 {
						continue
					}
					lat, e1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
					lon, e2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
					t, e3 := time.Parse(time.RFC3339, tp.Time)
					if e1 != nil || e2 != nil || e3 != nil {
						continue
					}
					if err := add(t.Unix(), lat, lon); err != nil {
						return total, days, err
					}
				}
			}
			if _, err := dec.Token(); err != nil {
				return total, days, err
			}
		default:
			// skip this value wholesale
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return total, days, err
			}
		}
	}
	if err := flush(); err != nil {
		return total, days, err
	}
	if total == 0 {
		return 0, days, fmt.Errorf("no recognisable location records (expected Takeout Records.json or Timeline semanticSegments)")
	}
	return total, days, nil
}
