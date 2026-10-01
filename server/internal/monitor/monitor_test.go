package monitor

import (
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/feedstat"
)

func TestPriceState(t *testing.T) {
	for _, c := range []struct {
		lag     int64
		have    int
		running int64
		want    string
	}{
		{-1, 0, 0, Waiting},
		{30, 60, 7200, OK},
		{30, 10, 600, OK}, // the box has taken minutes for ten minutes: ten is all there can be
		{30, 50, 7200, Flaky},
		{200, 60, 7200, Late},
		{1000, 60, 7200, Failing},
	} {
		if got := PriceState(c.lag, c.have, c.running); got != c.want {
			t.Errorf("PriceState(%d, %d, %d) = %s, want %s", c.lag, c.have, c.running, got, c.want)
		}
	}
}

func TestVenueState(t *testing.T) {
	now := time.Unix(1_790_900_000, 0)
	at := now.Unix() - 30
	good := feedstat.Stat{Calls: 60, OK: 60, P95Ms: 300, LastAt: at, LastOKAt: at, LastOK: true}
	for name, c := range map[string]struct {
		st   feedstat.Stat
		want string
	}{
		"never asked":    {feedstat.Stat{}, Waiting},
		"all good":       {good, OK},
		"some failed":    {func() feedstat.Stat { s := good; s.OK = 50; return s }(), Flaky},
		"three in a row": {func() feedstat.Stat { s := good; s.FailsInRow = 3; return s }(), Failing},
		"not asked":      {func() feedstat.Stat { s := good; s.LastAt, s.LastOKAt = now.Unix()-900, now.Unix()-900; return s }(), Failing},
		"no good since":  {func() feedstat.Stat { s := good; s.LastOKAt = now.Unix() - 700; return s }(), Failing},
		"slow":           {func() feedstat.Stat { s := good; s.P95Ms = 9000; return s }(), Late},
	} {
		if got := VenueState(c.st, now); got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
}

// The ECB's working days: the table is due at 16:00 Frankfurt (half an hour's grace), Monday to
// Friday; a weekend misses nothing.
func TestECBMissed(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, c := range []struct {
		newest, now string
		want        int
	}{
		{"2026-10-01", "2026-10-01T15:00:00Z", 0}, // Thursday 17:00 Frankfurt, today's is in
		{"2026-09-30", "2026-10-01T15:00:00Z", 1}, // today's due and not in
		{"2026-09-30", "2026-10-01T10:00:00Z", 0}, // 12:00 Frankfurt, today's not due yet
		{"2026-09-28", "2026-10-01T15:00:00Z", 3},
		{"2026-10-02", "2026-10-04T12:00:00Z", 0}, // Friday's on a Sunday
		{"2026-10-02", "2026-10-05T10:00:00Z", 0}, // Friday's on Monday morning
		{"2026-10-02", "2026-10-05T15:00:00Z", 1}, // Monday's due
		{"nonsense", "2026-10-05T15:00:00Z", -1},
	} {
		if got := ECBMissed(c.newest, at(c.now)); got != c.want {
			t.Errorf("ECBMissed(%s, %s) = %d, want %d", c.newest, c.now, got, c.want)
		}
	}
}

func TestNewsStateAndDigest(t *testing.T) {
	for _, c := range []struct {
		age              int64
		failing, enabled int
		want             string
	}{
		{-1, 0, 12, Waiting},
		{1800, 0, 0, Waiting},
		{1800, 0, 12, OK},
		{1800, 4, 12, Flaky},
		{10801, 0, 12, Late},
		{3*7200 + 1, 0, 12, Failing},
	} {
		if got := NewsState(c.age, c.failing, c.enabled); got != c.want {
			t.Errorf("NewsState(%d, %d, %d) = %s, want %s", c.age, c.failing, c.enabled, got, c.want)
		}
	}
	athens, err := time.LoadLocation("Europe/Athens")
	if err != nil {
		t.Skip("no zone data")
	}
	now := time.Date(2026, 10, 1, 9, 30, 0, 0, athens)
	if got := NextDigest(now, athens); !got.Equal(time.Date(2026, 10, 1, 19, 0, 0, 0, athens)) {
		t.Fatalf("morning: %v", got)
	}
	now = time.Date(2026, 10, 1, 20, 0, 0, 0, athens)
	if got := NextDigest(now, athens); !got.Equal(time.Date(2026, 10, 2, 7, 0, 0, 0, athens)) {
		t.Fatalf("evening: %v", got)
	}
}

func TestWordsAndSummary(t *testing.T) {
	for sec, want := range map[int64]string{-5: "?", 12: "12 s", 89: "89 s", 90: "2 min", 3599: "60 min", 5400: "2 h", 3 * 86400: "3 days"} {
		if got := Ago(sec); got != want {
			t.Errorf("Ago(%d) = %q, want %q", sec, got, want)
		}
	}
	if Ms(180) != "180 ms" || Ms(1234) != "1.2 s" {
		t.Fatal(Ms(180), Ms(1234))
	}
	for n, want := range map[int]string{999: "999", 10080: "10,080", 1000000: "1,000,000", -1234: "-1,234"} {
		if got := Count(n); got != want {
			t.Errorf("Count(%d) = %q", n, got)
		}
	}
	if Money(65012.4) != "65,012" || Money(3.5) != "3.50" || Money(0.5) != "0.5000" {
		t.Fatal(Money(65012.4), Money(3.5), Money(0.5))
	}
	if Reason("stale (3m0s old)") != "stale" || Reason("off by 2.4% from the median") != "too far from the others" || Reason("no volume given") != "no volume given" {
		t.Fatal("reasons")
	}
	if Worst() != OK || Worst(OK, Filling, Flaky) != Flaky || Worst(Late, Failing, OK) != Failing {
		t.Fatal("worst")
	}
	secs := []Section{{Title: "Prices", State: OK}, {Title: "Price history", State: Filling}}
	if got := Summary(secs); got != "all well, still filling price history" {
		t.Fatal(got)
	}
	secs = append(secs, Section{Title: "Exchanges", State: Flaky})
	if got := Summary(secs); got != "1 wants a look: exchanges flaky" {
		t.Fatal(got)
	}
	secs = append(secs, Section{Title: "News", State: Late})
	if got := Summary(secs); got != "2 want a look: exchanges flaky, news late" {
		t.Fatal(got)
	}
	if got := Summary([]Section{{Title: "Prices", State: OK}}); got != "all well" {
		t.Fatal(got)
	}
}
