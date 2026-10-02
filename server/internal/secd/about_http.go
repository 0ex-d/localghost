package secd

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/hw"
)

// aboutDoc is /v1/about: the note I wrote about me and my people, the name it gives, and how many
// memories the box made from it (synthd reads the note again when it changes, within ten minutes).
type aboutDoc struct {
	Text      string `json:"text"`
	UpdatedAt int64  `json:"updatedAt"`
	Name      string `json:"name"`
	Me        int    `json:"me"`     // memories about me from the note
	People    int    `json:"people"` // people the box knows by name (the note's and the chats')
	Pending   bool   `json:"pending"`
}

const aboutMax = 8000

// handleAbout , GET /v1/about reads the note; POST {"text":"…"} writes it (8,000 characters at
// most). The box makes memories from it at synthd's next pass: one per person, and facts about me.
func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	if !s.session.Valid(bearer(r)) || (r.Method != http.MethodGet && r.Method != http.MethodPost) {
		s.appearsDown(w)
		return
	}
	mounted, ok := s.mountedSlot()
	if !ok {
		s.appearsDown(w)
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil || len(req.Text) > aboutMax {
			http.Error(w, "text, 8000 characters at most", http.StatusBadRequest)
			return
		}
		if err := s.notif.SetSetting(mounted, "about_me", strings.TrimSpace(req.Text)); err != nil {
			s.appearsDown(w)
			return
		}
		_ = s.notif.SetSetting(mounted, "about_me_at", strconv.FormatInt(time.Now().Unix(), 10))
	}
	d := aboutDoc{}
	d.Text, _ = s.notif.GetSetting(mounted, "about_me")
	at, _ := s.notif.GetSetting(mounted, "about_me_at")
	d.UpdatedAt, _ = strconv.ParseInt(at, 10, 64)
	d.Name, _ = s.notif.GetSetting(mounted, "owner_name")
	if db, err := s.notif.DB(mounted); err == nil {
		count := func(q string) int {
			rows, err := db.Query(q)
			if err != nil || len(rows.Vals) != 1 || rows.Vals[0][0] == nil {
				return 0
			}
			n, _ := strconv.Atoi(*rows.Vals[0][0])
			return n
		}
		d.Me = count("SELECT count(*) FROM memories WHERE kind = 'me' AND NOT tombstoned")
		d.People = count("SELECT count(DISTINCT lower(title)) FROM memories WHERE kind = 'person' AND NOT tombstoned")
	}
	done, _ := s.notif.GetSetting(mounted, "about_me_distilled")
	d.Pending = d.Text != "" && done != hw.AboutHash(d.Text) // the version in it: memories made the older way are pending too
	writeJSON(w, d)
}
