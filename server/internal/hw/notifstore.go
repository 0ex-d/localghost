package hw

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// NotifStore is the per-account notification data model, living INSIDE the encrypted volume (Redis
// last-100 hot cache + Postgres durable history). Notifications are always PRODUCED by the daemons
// (synthd, shadowd, cued, ...) regardless of mute , mute only affects what the poller PUSHES, not what
// is stored. Every notification persists in Postgres with a seen flag and can be deleted forever; the
// in-app list reads the full history, the poller reads only the push window.
//
// Push model (Option A): a per-DEVICE cursor (highest id pushed to that device) is a high-water mark
// in Redis. On a poll, ghost.secd reads the last-100 newer than the cursor, advances the cursor past
// ALL of them (muted included), then drops the muted ones from the returned payload. So a muted
// notification is skipped from push and never pushed later (the cursor moved past it), but it remains
// in the store with its seen/delete state for the in-app list. This is why the cursor must advance
// over the full not-yet-pushed set BEFORE muted ones are removed.
//
// Shell-out to redis-cli / psql (no DB driver dependency), matching DataStore/MuteStore.
//
// Redis:  notifications:recent       LIST of JSON notifications (newest first), trimmed to 100
//         notifications:cursor:<dev>  highest pushed id for a device
// Postgres: notifications(id, service, kind, title, body, seen, created)
//
// NOT validated in CI; exercise on the box.

const recentKey = "notifications:recent"
const recentCap = 100

// Ask errors, surfaced by the answer endpoint so the app can tell a stale/duplicate answer from a bad one.
var (
	ErrNoNotif         = notifErr("no such notification")
	ErrNotAsk          = notifErr("notification is not an ask")
	ErrBadChoice       = notifErr("choice is not one of the offered options")
	ErrAlreadyAnswered = notifErr("ask already answered")
)

type notifErr string

func (e notifErr) Error() string { return string(e) }

type Notification struct {
	ID      int64  `json:"id"`
	Service string `json:"service"`
	Kind    string `json:"kind"` // e.g. "message", "alert" , lets the app render generic vs specific
	Title   string `json:"title"`
	Body    string `json:"body"`
	Seen    bool   `json:"seen"`
	// Ask fields. A notification with Options is an "ask" the user can answer (ghost.cued nominations
	// and confirmations); one without is passive. Answer is the chosen option once picked, Answered
	// its unix time (0 = still pending). The app renders Options as buttons and posts the choice back.
	Options  []string `json:"options,omitempty"`
	Answer   string   `json:"answer,omitempty"`
	Answered int64    `json:"answered,omitempty"`
	Created  int64    `json:"created"` // unix seconds
}

// IsAsk reports whether this notification expects an answer (it carries options).
func (n Notification) IsAsk() bool { return len(n.Options) > 0 }

type NotifStore struct {
	pgSocketFor func(slot int) string
}

func NewNotifStore(pgSocketFor func(slot int) string) *NotifStore {
	return &NotifStore{pgSocketFor: pgSocketFor}
}

// Produce stores a new notification: insert into Postgres (durable, returns the assigned id), then
// LPUSH the JSON onto the Redis last-100 list and trim. Called by the daemons; mute does NOT gate this
// (notifications are always produced and stored).
func (s *NotifStore) Produce(slot int, n Notification) error {
	id, err := s.insertPostgres(slot, n)
	if err != nil {
		return err
	}
	n.ID = id
	if n.Created == 0 {
		n.Created = time.Now().Unix()
	}
	blob, err := json.Marshal(n)
	if err != nil {
		return err
	}
	if err := s.redis(slot, "LPUSH", recentKey, string(blob)); err != nil {
		return err
	}
	return s.redis(slot, "LTRIM", recentKey, "0", strconv.Itoa(recentCap-1))
}

// PushBatch is what the poller calls: returns the notifications to push to `device` (those newer than
// the device cursor, with muted scopes removed), and advances the cursor past the FULL not-yet-pushed
// set (muted included) so muted ones are never pushed later. muted(service) decides per-service; a nil
// muted func means nothing is muted.
func (s *NotifStore) PushBatch(slot int, device string, muted func(service string) bool) ([]Notification, error) {
	recent, err := s.readRecent(slot)
	if err != nil {
		return nil, err
	}
	cursor, _ := s.getCursor(slot, device) // 0 if unset

	// (2) not-yet-pushed = id > cursor. Track the max id across ALL of them for the cursor advance.
	var maxID int64 = cursor
	fresh := make([]Notification, 0, len(recent))
	for _, n := range recent {
		if n.ID <= cursor {
			continue
		}
		if n.ID > maxID {
			maxID = n.ID
		}
		fresh = append(fresh, n)
	}

	// (5) advance the cursor past everything fresh, BEFORE removing muted , so muted ones are passed
	// over (Option A: skipped from push, not delivered later).
	if maxID > cursor {
		if err := s.setCursor(slot, device, maxID); err != nil {
			return nil, err
		}
	}

	// (3) drop muted from the returned payload.
	out := make([]Notification, 0, len(fresh))
	for _, n := range fresh {
		if muted != nil && muted(n.Service) {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

// List returns the in-app notification history (newest first, up to limit), from Postgres , the full
// list regardless of mute or push cursor, with seen/delete and ask (options/answer) state.
func (s *NotifStore) List(slot int, limit int) ([]Notification, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := fmt.Sprintf("SELECT id, service, kind, title, body, seen, "+
		"coalesce(options,''), coalesce(answer,''), coalesce(extract(epoch from answered)::bigint,0), "+
		"extract(epoch from created)::bigint "+
		"FROM notifications ORDER BY id DESC LIMIT %d;", limit)
	out, err := s.psqlQuery(slot, q)
	if err != nil {
		return nil, err
	}
	res := make([]Notification, 0, len(out))
	for _, row := range out {
		f := strings.Split(row, "|")
		if len(f) < 10 {
			continue
		}
		id, _ := strconv.ParseInt(f[0], 10, 64)
		answered, _ := strconv.ParseInt(f[8], 10, 64)
		created, _ := strconv.ParseInt(f[9], 10, 64)
		n := Notification{
			ID: id, Service: f[1], Kind: f[2], Title: f[3], Body: f[4],
			Seen: f[5] == "t", Answer: f[7], Answered: answered, Created: created,
		}
		if f[6] != "" {
			_ = json.Unmarshal([]byte(f[6]), &n.Options) // options is a JSON array; ignore if malformed
		}
		res = append(res, n)
	}
	return res, nil
}

// Answer records the user's choice for an ask. It fetches the stored options for that id, rejects a
// choice that is not one of them (and rejects answering a non-ask or an already-answered one), then
// writes answer + answered. Postgres is the source of truth for answered state, same as seen; the
// Redis push cache is not rewritten (the ask was already pushed, and the app reads answered state
// from the list). Returns ErrNotAsk / ErrBadChoice / ErrAlreadyAnswered for the endpoint to surface.
func (s *NotifStore) Answer(slot int, id int64, choice string) error {
	rows, err := s.psqlQuery(slot, fmt.Sprintf(
		"SELECT coalesce(options,''), coalesce(answer,'') FROM notifications WHERE id = %d;", id))
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return ErrNoNotif
	}
	f := strings.SplitN(rows[0], "|", 2)
	optionsJSON := f[0]
	prevAnswer := ""
	if len(f) > 1 {
		prevAnswer = f[1]
	}
	if optionsJSON == "" {
		return ErrNotAsk
	}
	var options []string
	if json.Unmarshal([]byte(optionsJSON), &options) != nil || len(options) == 0 {
		return ErrNotAsk
	}
	if prevAnswer != "" {
		return ErrAlreadyAnswered
	}
	valid := false
	for _, o := range options {
		if o == choice {
			valid = true
			break
		}
	}
	if !valid {
		return ErrBadChoice
	}
	return s.psqlExec(slot, fmt.Sprintf(
		"UPDATE notifications SET answer = '%s', answered = now() WHERE id = %d;", esc(choice), id))
}

// MarkSeen sets the seen flag on a notification (the app calls this when it is viewed).
func (s *NotifStore) MarkSeen(slot int, id int64) error {
	return s.psqlExec(slot, fmt.Sprintf("UPDATE notifications SET seen = TRUE WHERE id = %d;", id))
}

// Delete removes a notification forever (Postgres). It stays in the Redis last-100 until it ages out
// of the window; the poller filters deleted ids out by virtue of the cursor having passed them, and
// the in-app list reads Postgres, so a deleted one is gone from the list immediately.
func (s *NotifStore) Delete(slot int, id int64) error {
	return s.psqlExec(slot, fmt.Sprintf("DELETE FROM notifications WHERE id = %d;", id))
}

// --- helpers ---

func (s *NotifStore) readRecent(slot int) ([]Notification, error) {
	out, err := exec.Command("redis-cli", "-p", strconv.Itoa(redisPort(slot)), "LRANGE", recentKey, "0", "-1").Output()
	if err != nil {
		return nil, fmt.Errorf("read recent: %w", err)
	}
	var res []Notification
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var n Notification
		if json.Unmarshal([]byte(line), &n) == nil {
			res = append(res, n)
		}
	}
	return res, nil
}

func (s *NotifStore) getCursor(slot int, device string) (int64, error) {
	out, err := exec.Command("redis-cli", "-p", strconv.Itoa(redisPort(slot)), "GET", cursorKey(device)).Output()
	if err != nil {
		return 0, err
	}
	v := strings.TrimSpace(string(out))
	if v == "" || v == "(nil)" {
		return 0, nil
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, nil
	}
	return id, nil
}

func (s *NotifStore) setCursor(slot int, device string, id int64) error {
	return s.redis(slot, "SET", cursorKey(device), strconv.FormatInt(id, 10))
}

func cursorKey(device string) string { return "notifications:cursor:" + device }

func (s *NotifStore) insertPostgres(slot int, n Notification) (int64, error) {
	created := n.Created
	if created == 0 {
		created = time.Now().Unix()
	}
	// options is stored as a JSON array of choices (empty string when this is not an ask). A freshly
	// produced ask is always pending: answer='' answered NULL.
	optionsJSON := ""
	if len(n.Options) > 0 {
		b, err := json.Marshal(n.Options)
		if err != nil {
			return 0, fmt.Errorf("marshal options: %w", err)
		}
		optionsJSON = string(b)
	}
	q := fmt.Sprintf(
		"INSERT INTO notifications (service, kind, title, body, seen, options, created) "+
			"VALUES ('%s','%s','%s','%s', FALSE, '%s', to_timestamp(%d)) RETURNING id;",
		esc(n.Service), esc(n.Kind), esc(n.Title), esc(n.Body), esc(optionsJSON), created)
	rows, err := s.psqlQuery(slot, q)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("insert returned no id")
	}
	id, err := strconv.ParseInt(strings.TrimSpace(rows[0]), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("bad id: %w", err)
	}
	return id, nil
}

func (s *NotifStore) redis(slot int, args ...string) error {
	full := append([]string{"-p", strconv.Itoa(redisPort(slot))}, args...)
	out, err := exec.Command("redis-cli", full...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("redis %v: %v: %s", args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *NotifStore) psqlQuery(slot int, q string) ([]string, error) {
	sock := s.pgSocketFor(slot)
	out, err := exec.Command("psql", "-h", sock, "-p", strconv.Itoa(pgPort(slot)), "-U", "ghost",
		"-d", "ghost", "-At", "-F", "|", "-c", q).Output()
	if err != nil {
		return nil, fmt.Errorf("psql query: %w", err)
	}
	var rows []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(l) != "" {
			rows = append(rows, l)
		}
	}
	return rows, nil
}

func (s *NotifStore) psqlExec(slot int, stmt string) error {
	sock := s.pgSocketFor(slot)
	out, err := exec.Command("psql", "-h", sock, "-p", strconv.Itoa(pgPort(slot)), "-U", "ghost",
		"-d", "ghost", "-v", "ON_ERROR_STOP=1", "-c", stmt).CombinedOutput()
	if err != nil {
		return fmt.Errorf("psql exec: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// esc doubles single quotes for SQL literals. Notification content comes from the daemons (trusted,
// in-volume), not the network; this guards against quotes in titles/bodies breaking the statement.
func esc(s string) string { return strings.ReplaceAll(s, "'", "''") }
