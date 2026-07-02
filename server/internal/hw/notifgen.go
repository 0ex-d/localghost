package hw

import (
	"fmt"
	"math/rand"
	"time"
)

// NotifGen makes believable notifications for development, so the notification surface (the list, the
// mute filtering, the per-device cursor, seen, delete) has something real-looking to show while the
// actual daemons are still stubs. It is templates, not a model. The box does not run inference (it
// serves weights for the phone to run), and seed data does not need a 12B model anyway , a handful of
// per-service templates with randomised fields gives variety with no dependency and no latency.
//
// This is a DEV tool. When the real daemons (synthd, shadowd, ...) do their jobs, they call
// NotifStore.Produce directly and this goes away.

// serviceTemplates maps each notification-producing daemon to the kind of thing it would plausibly
// say. The daemon names match notificationServices in secd. Each template has a kind (drives the
// app's generic-vs-specific render), a set of titles, a set of bodies, and optional options; Seed
// picks at random. A template WITH options produces an "ask" (answerable); without, a passive note.
var serviceTemplates = map[string]struct {
	kind    string
	titles  []string
	bodies  []string
	options []string // non-empty => this service asks (renders as buttons, answered via /answer)
}{
	"ghost.noted": {
		kind:   "note",
		titles: []string{"Note saved", "Idea captured", "Reminder set"},
		bodies: []string{"Pick up the dry cleaning before six.", "That thing about the garden fence.", "Call the dentist back."},
	},
	"ghost.framed": {
		kind:   "photo",
		titles: []string{"Photos synced", "New album", "Backup complete"},
		bodies: []string{"42 photos from today are on the box.", "Last weekend's pictures are in.", "Camera roll is backed up."},
	},
	"ghost.voiced": {
		kind:   "message",
		titles: []string{"New message", "Voice note", "Someone replied"},
		bodies: []string{"Mum asked about the weekend.", "A voice note is waiting.", "Reply came in on the group thread."},
	},
	"ghost.tallyd": {
		kind:   "finance",
		titles: []string{"Spending update", "Bill due", "Balance changed"},
		bodies: []string{"Electricity bill due Thursday.", "You spent more on coffee this week.", "A subscription renewed."},
	},
	"ghost.synthd": {
		kind:   "digest",
		titles: []string{"Daily digest", "Summary ready", "Catch-up"},
		bodies: []string{"Three things happened today worth a look.", "Your week in one paragraph is ready.", "Nothing urgent, a quiet day."},
	},
	// ghost.cued is the cueing daemon: it reads the user's context and asks ghost.synthd to surface
	// the right memory at the right moment, before a question forms , quiet, rare, and on time (see
	// the "Before You Ask" Hard Truth). Most cues are surfaced memories; SOME are asks that need a
	// yes/no, and those carry options so the app renders buttons and posts the choice to
	// /v1/notifications/answer. The shoebox nomination below is ONE such ask (the first concrete
	// producer, ~5-10% of the day's photos "worth never losing"), not the whole of what ghost.cued
	// does. Dev seed uses it because it is the one ask with settled copy; real cues build their body
	// from the specific moment.
	"ghost.cued": {
		kind:    "ask",
		titles:  []string{"Keep this one?", "Worth keeping?", "One for the shoebox?"},
		bodies:  []string{"A favourite from today , keep it in the plain shoebox so a lost code can never erase it?", "This looked like a load-bearing memory. Keep it where it always survives?", "Best shot of the day. Add it to the never-lose set?"},
		options: []string{"Keep", "Skip"},
	},
	"ghost.mistd": {
		kind:   "weather",
		titles: []string{"Weather", "Heads up", "Forecast"},
		bodies: []string{"Rain this afternoon, take a coat.", "Clear tonight, good for the walk.", "Frost expected by morning."},
	},
	"ghost.shadowd": {
		kind:   "alert",
		titles: []string{"Security note", "Login", "Heads up"},
		bodies: []string{"A new device asked to enrol.", "Sign-in from the usual place.", "Nothing suspicious to report."},
	},
	"ghost.watchd": {
		kind:   "system",
		titles: []string{"Box health", "Status", "All good"},
		bodies: []string{"Disks healthy, nodes in sync.", "One service restarted cleanly.", "Backups ran overnight."},
	},
}

type NotifGen struct {
	store *NotifStore
	rnd   *rand.Rand
}

func NewNotifGen(store *NotifStore) *NotifGen {
	return &NotifGen{store: store, rnd: rand.New(rand.NewSource(time.Now().UnixNano()))}
}

// Seed produces n believable notifications into the slot, drawn from random services. Returns how many
// were stored. Each one goes through NotifStore.Produce, so it exercises the real path (Postgres insert
// + Redis last-100), not a shortcut.
func (g *NotifGen) Seed(slot, n int) (int, error) {
	services := make([]string, 0, len(serviceTemplates))
	for s := range serviceTemplates {
		services = append(services, s)
	}
	stored := 0
	for i := 0; i < n; i++ {
		svc := services[g.rnd.Intn(len(services))]
		note := g.one(svc)
		if err := g.store.Produce(slot, note); err != nil {
			return stored, fmt.Errorf("produce %d/%d: %w", i+1, n, err)
		}
		stored++
	}
	return stored, nil
}

// SeedService produces n notifications from one named service (handy for testing per-service mute).
func (g *NotifGen) SeedService(slot int, service string, n int) (int, error) {
	if _, ok := serviceTemplates[service]; !ok {
		return 0, fmt.Errorf("unknown service %q", service)
	}
	stored := 0
	for i := 0; i < n; i++ {
		if err := g.store.Produce(slot, g.one(service)); err != nil {
			return stored, err
		}
		stored++
	}
	return stored, nil
}

func (g *NotifGen) one(service string) Notification {
	t := serviceTemplates[service]
	return Notification{
		Service: service,
		Kind:    t.kind,
		Title:   t.titles[g.rnd.Intn(len(t.titles))],
		Body:    t.bodies[g.rnd.Intn(len(t.bodies))],
		Options: t.options, // nil for passive services; set makes it an ask
		Created: time.Now().Unix(),
	}
}
