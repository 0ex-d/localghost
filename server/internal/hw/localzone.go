package hw

import (
	"time"
	_ "time/tzdata" // the zone rules ride in the binary: a box with no tzdata package still knows Athens
)

// LocalZone is the person's time zone as the box knows it: settings local_tz, which ghost.framed
// writes from the trail's newest point (internal/tzgrid), else the box's own clock's zone. The
// digests' hours, "today" in the day stories and cued's quiet hours are read in this zone, so a
// person in Athens with a box in London gets 19:00 in Athens.
func LocalZone(c Querier) *time.Location {
	if c != nil {
		rows, err := c.Query("SELECT value FROM settings WHERE key = 'local_tz'")
		if err == nil && len(rows.Vals) == 1 && len(rows.Vals[0]) > 0 && rows.Vals[0][0] != nil {
			if loc, err := time.LoadLocation(*rows.Vals[0][0]); err == nil {
				return loc
			}
		}
	}
	return time.Local
}
