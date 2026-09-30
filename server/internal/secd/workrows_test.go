package secd

import (
	"strings"
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/hw"
	"github.com/LocalGhostDao/localghost/server/internal/workcount"
)

func rowsText(rows []hw.DaemonKV) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(r.K + "=" + r.V + "\n")
	}
	return b.String()
}

func TestWorkRowsNameAndOrderTheKinds(t *testing.T) {
	now := time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	w := workcount.Snapshot{Since: now.Add(-3*time.Hour - 10*time.Minute).Unix(), Kinds: []workcount.Kind{
		{Kind: "caption", Hour: 42, Day: 1310, Total: 1310, LastAt: now.Add(-20 * time.Second).Unix()},
		{Kind: "tag", Hour: 40, Day: 1290, Total: 1290, LastAt: now.Add(-50 * time.Second).Unix()},
		{Kind: "zzz new kind", Hour: 1, Day: 1, Total: 1},
	}}
	got := rowsText(formatWork("ghost.searchd", w, now, nil))
	for _, want := range []string{
		"counting since=the daemon started, 3 h 10 min ago",
		"photos described=42 last hour · 1,310 since start · last 20 s ago",
		"photos named and tagged=40 last hour · 1,290 since start",
		"zzz new kind=1 last hour",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in\n%s", want, got)
		}
	}
	if strings.Index(got, "photos described") > strings.Index(got, "photos named") || strings.Index(got, "photos named") > strings.Index(got, "zzz") {
		t.Fatalf("order:\n%s", got)
	}
	// over a day up: the last 24 h is its own figure
	w.Since = now.Add(-30 * time.Hour).Unix()
	if got := rowsText(formatWork("ghost.searchd", w, now, nil)); !strings.Contains(got, "42 last hour · 1,310 last 24 h · 1,310 since start") {
		t.Fatalf("a day up:\n%s", got)
	}
	if got := rowsText(formatWork("ghost.framed", workcount.Snapshot{Since: now.Unix()}, now, nil)); !strings.Contains(got, "done=nothing yet since it started") {
		t.Fatalf("empty:\n%s", got)
	}
}

func TestGPUUseRows(t *testing.T) {
	on, off := true, false
	got := rowsText(formatGPUUse("gemma-4-12b", true, true, &on, "", 9064, 12282))
	for _, want := range []string{"model=gemma-4-12b · on the card · holds 8.9 GB of 12.0 GB", "sees photos=yes", "on the CPU by choice=search embeddings and voice transcription"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in\n%s", want, got)
		}
	}
	got = rowsText(formatGPUUse("gemma-4-12b", true, true, &off, "the projector is switched off by hand (mmproj-F16.gguf.off)", 8700, 0))
	if !strings.Contains(got, "sees photos=NO , the projector is switched off by hand") || !strings.Contains(got, "holds 8.5 GB\n") {
		t.Fatalf("no vision:\n%s", got)
	}
	if got := rowsText(formatGPUUse("gemma-4-12b", false, false, nil, "", 0, 0)); strings.Contains(got, "sees photos") || !strings.Contains(got, "loading") {
		t.Fatalf("older oracled:\n%s", got)
	}
}

func TestThousandsAndSpan(t *testing.T) {
	for in, want := range map[int64]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567"} {
		if thousands(in) != want {
			t.Fatalf("%d: %s", in, thousands(in))
		}
	}
	if span(50*time.Hour) != "2 days 2 h" || span(90*time.Second) != "1 min" || span(125*time.Minute) != "2 h 5 min" {
		t.Fatal("span")
	}
}
