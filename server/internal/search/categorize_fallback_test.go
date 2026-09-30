package search

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/ctlsock"
	"github.com/LocalGhostDao/localghost/server/internal/oracle"
)

// A chunk the model does not see as tags ("Please provide the list of tags") is asked tag by tag;
// the odd one out that still gets no pair is "other", and the job no longer fails for good.
func TestCategorizeAsksOddTagsAlone(t *testing.T) {
	dir, err := os.MkdirTemp("", "lgcat")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	var asked []string
	srv := ctlsock.NewServer("ghost.oracled", dir, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	srv.Handle("infer", func(args json.RawMessage) (ctlsock.Response, error) {
		var req oracle.Request
		_ = json.Unmarshal(args, &req)
		list := strings.TrimPrefix(req.Input, CategorizePrompt)
		asked = append(asked, list)
		out := "Please provide the list of tags you would like me to categorize! You haven't included them in your message."
		switch list {
		case "beach":
			out = "place:beach"
		case "boat":
			out = "vehicle:boat"
		}
		data, _ := json.Marshal(oracle.Response{Output: out})
		return ctlsock.Response{OK: true, Data: data}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()
	defer srv.Cleanup()
	time.Sleep(100 * time.Millisecond)

	tg := &TagOracle{Client: oracle.NewClient(dir, 5*time.Second)}
	got, err := tg.Categorize(context.Background(), []string{"beach", "~~", "boat"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"beach": "place", "~~": OtherCategory, "boat": "vehicle"}
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	for _, g := range got {
		if want[g.Name] != g.Category {
			t.Fatalf("%q is %q, want %q (all: %+v)", g.Name, g.Category, want[g.Name], got)
		}
	}
	if strings.Join(asked, "|") != "beach, ~~, boat|beach|~~|boat" {
		t.Fatalf("asked %q", asked)
	}
}
